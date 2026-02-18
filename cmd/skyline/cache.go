package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/mcp"
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
	s.redactor.AddSecrets(cfg.Secrets())

	services, err := spec.LoadServices(ctx, cfg, s.logger, s.redactor)
	if err != nil {
		return nil, false, fmt.Errorf("load services: %w", err)
	}

	services = spec.ApplyOperationFilters(services, cfg.APIs)

	registry, err := mcp.NewRegistry(services)
	if err != nil {
		return nil, false, fmt.Errorf("build registry: %w", err)
	}

	executor, err := runtime.NewExecutor(cfg, services, s.logger, s.redactor)
	if err != nil {
		return nil, false, fmt.Errorf("create executor: %w", err)
	}

	return &registryCache{
		registry:  registry,
		executor:  executor,
		services:  services,
		createdAt: time.Now(),
	}, false, nil
}
