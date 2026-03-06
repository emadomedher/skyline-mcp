package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"log/slog"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
	"skyline-mcp/internal/email"
	"skyline-mcp/internal/mcp"
	"skyline-mcp/internal/polling"
	"skyline-mcp/internal/runtime"
	"skyline-mcp/internal/spec"
)

// registryCache holds a cached registry and executor for a profile.
type registryCache struct {
	registry   *mcp.Registry
	executor   *runtime.Executor
	services   []*canonical.Service
	configHash string
	createdAt  time.Time
}

// profileCache manages per-profile caches of parsed specs, registries, and executors.
type profileCache struct {
	mu      sync.RWMutex
	entries map[string]*registryCache
	ttl     time.Duration
}

func newProfileCache(ttl time.Duration) *profileCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &profileCache{
		entries: make(map[string]*registryCache),
		ttl:     ttl,
	}
}

// profileConfigHash returns a SHA-256 hash of the profile's ConfigYAML for cache invalidation.
func profileConfigHash(configYAML string) string {
	h := sha256.Sum256([]byte(configYAML))
	return fmt.Sprintf("%x", h)
}

// get returns a cached entry if it exists, the config hash matches, and it hasn't expired.
func (pc *profileCache) get(profileName, configHash string) (*registryCache, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	entry, ok := pc.entries[profileName]
	if !ok {
		return nil, false
	}
	if entry.configHash != configHash {
		return nil, false
	}
	if time.Since(entry.createdAt) > pc.ttl {
		return nil, false
	}
	return entry, true
}

// set stores a cache entry for the given profile.
func (pc *profileCache) set(profileName string, entry *registryCache) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.entries[profileName] = entry
}

// evict removes the cache entry for the given profile.
func (pc *profileCache) evict(profileName string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	delete(pc.entries, profileName)
}

// getOrBuild returns a cached registry/executor or builds a new one.
// Returns (cache entry, hit, error).
func (s *server) getOrBuildCache(ctx context.Context, prof profile) (*registryCache, bool, error) {
	if s.cache == nil {
		return s.buildRegistryCache(ctx, prof)
	}

	hash := profileConfigHash(prof.ConfigYAML)
	if entry, ok := s.cache.get(prof.Name, hash); ok {
		s.metrics.RecordCacheHit()
		return entry, true, nil
	}

	s.metrics.RecordCacheMiss()
	entry, _, err := s.buildRegistryCache(ctx, prof)
	if err != nil {
		return nil, false, err
	}
	entry.configHash = hash
	s.cache.set(prof.Name, entry)
	return entry, false, nil
}

// buildRegistryCache builds a fresh registry cache entry for a profile.
func (s *server) buildRegistryCache(ctx context.Context, prof profile) (*registryCache, bool, error) {
	cfg := prof.ToConfig()
	// Strip disabled APIs before building the registry/executor
	active := cfg.APIs[:0]
	for _, api := range cfg.APIs {
		if !api.Disabled {
			active = append(active, api)
		}
	}
	cfg.APIs = active
	s.redactor.AddSecrets(cfg.Secrets())

	services, err := spec.LoadServices(ctx, cfg, s.logger, s.redactor)
	if err != nil {
		return nil, false, fmt.Errorf("load services: %w", err)
	}

	registry, err := mcp.NewRegistry(services)
	if err != nil {
		return nil, false, fmt.Errorf("build registry: %w", err)
	}

	executor, err := runtime.NewExecutor(cfg, services, s.logger, s.redactor)
	if err != nil {
		return nil, false, fmt.Errorf("create executor: %w", err)
	}

	// Register email protocol handler if any email-type APIs exist.
	registerEmailProtocol(executor, cfg, s.logger, s.emailPersistent)

	// Register email inbox resources for persistent-mode accounts
	registerEmailResources(registry, cfg)

	// Register email inbox polling for APIs with poll_interval_seconds > 0.
	if s.pollEngine != nil {
		registerEmailPolling(s.pollEngine, cfg, s.logger)
	}

	return &registryCache{
		registry:  registry,
		executor:  executor,
		services:  services,
		createdAt: time.Now(),
	}, false, nil
}

// registerEmailPolling sets up poll jobs for email APIs with polling enabled.
func registerEmailPolling(engine *polling.Engine, cfg *config.Config, logger *slog.Logger) {
	for _, api := range cfg.APIs {
		if api.SpecType != "email" || api.Email == nil {
			continue
		}
		if api.Email.PollIntervalSeconds <= 0 {
			continue
		}
		emailCfg := email.ConfigFromAPIConfig(api.Email)
		if emailCfg.ReadProtocol() == "" {
			continue // no IMAP/POP3 configured
		}
		source := polling.NewEmailInboxSource(emailCfg, logger)
		interval := time.Duration(api.Email.PollIntervalSeconds) * time.Second
		engine.Register(source, interval)
		logger.Info("email polling enabled", "api", api.Name, "address", api.Email.Address, "interval", interval)
	}
}

// registerEmailResources adds email inbox resources to the MCP registry
// for email APIs, enabling resource subscriptions for new-email notifications.
func registerEmailResources(registry *mcp.Registry, cfg *config.Config) {
	for _, api := range cfg.APIs {
		if api.SpecType != "email" || api.Email == nil {
			continue
		}
		emailCfg := email.ConfigFromAPIConfig(api.Email)
		if emailCfg.ReadProtocol() != "imap" {
			continue
		}
		uri := email.InboxURI(api.Name)
		// Find the actual tool name (may be renamed by CRUD grouping)
		listToolName := api.Name + "__list_emails"
		if _, ok := registry.Tools[listToolName]; !ok {
			// CRUD grouping renamed it — look for a messages_manage composite
			if _, ok := registry.Tools[api.Name+"__messages_manage"]; ok {
				listToolName = api.Name + "__messages_manage"
			}
		}
		registry.Resources[uri] = &mcp.Resource{
			URI:         uri,
			Name:        api.Name + " inbox",
			MimeType:    "application/json",
			Description: "Email inbox for " + emailCfg.Address + " — subscribe for new email notifications",
			ToolName:    listToolName,
			DefaultArgs: map[string]any{"action": "list", "folder": "INBOX", "limit": 20},
		}
	}
}

// registerEmailProtocol registers the email protocol handler on an executor
// for any email-type APIs in the config. Shared by cache and transport paths.
func registerEmailProtocol(executor *runtime.Executor, cfg *config.Config, logger *slog.Logger, pm *email.PersistentManager) {
	emailConfigs := map[string]*email.EmailConfig{}
	for _, api := range cfg.APIs {
		if api.SpecType == "email" && api.Email != nil {
			emailConfigs[api.Name] = email.ConfigFromAPIConfig(api.Email)
		}
	}
	if len(emailConfigs) > 0 {
		executor.RegisterProtocol("email", func(ctx context.Context, op *canonical.Operation, args map[string]any) (*runtime.Result, error) {
			emailCfg, ok := emailConfigs[op.ServiceName]
			if !ok {
				return nil, fmt.Errorf("no email config for service %s", op.ServiceName)
			}
			// Use connection pool from persistent manager if available
			var pool *email.IMAPPool
			if pm != nil {
				pool = pm.GetPool(op.ServiceName)
			}
			return email.ExecuteEmailTool(ctx, op, args, emailCfg, logger, pool)
		})
	}
}
