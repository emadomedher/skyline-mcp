package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"skyline-mcp/internal/config"
	"skyline-mcp/internal/mcp"
)

// handleProfileMCP handles Streamable HTTP MCP connections for a profile.
// This allows MCP clients (e.g. Codex, Claude Code) to connect via:
//
//	POST/GET/DELETE /profiles/{name}/mcp
func (s *server) handleProfileMCP(w http.ResponseWriter, r *http.Request) {
	name := extractProfileName(r.URL.Path, "/profiles/", "/mcp")
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}

	// Look up profile
	s.mu.RLock()
	prof, ok := s.findProfile(name)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Get or build the StreamableHTTPServer for this profile
	streamable, err := s.getOrCreateStreamable(r.Context(), prof)
	if err != nil {
		http.Error(w, fmt.Sprintf("load services: %v", err), http.StatusInternalServerError)
		return
	}

	// Delegate to StreamableHTTPServer (implements http.Handler)
	streamable.ServeHTTP(w, r)
}

// getOrCreateStreamable returns a cached StreamableHTTPServer for the profile,
// creating one if it doesn't exist or the config has changed.
func (s *server) getOrCreateStreamable(ctx context.Context, prof profile) (*mcp.StreamableHTTPServer, error) {
	hash := profileConfigHash(prof.ConfigYAML)
	cacheKey := prof.Name + ":" + hash

	// Check cache
	if val, ok := s.mcpServers.Load(cacheKey); ok {
		return val.(*mcp.StreamableHTTPServer), nil
	}

	// Build registry and executor from profile config
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cached, _, err := s.getOrBuildCache(ctx, prof)
	if err != nil {
		return nil, err
	}

	// Create MCP server for this profile
	mcpServer := mcp.NewServer(cached.registry, cached.executor, s.logger, s.redactor, Version)

	// Apply per-API response truncation limits
	profCfg := prof.ToConfig()
	apiLimits := make(map[string]int, len(profCfg.APIs))
	for _, api := range profCfg.APIs {
		if api.MaxResponseBytes != nil {
			apiLimits[api.Name] = *api.MaxResponseBytes
		}
	}
	mcpServer.SetMaxResponseBytesByAPI(apiLimits)

	// Wire up audit logging + metrics for MCP tool calls
	profileName := prof.Name

	// Fire before tool execution — update real-time activity tracking
	mcpServer.SetToolCallStartHook(func(ctx context.Context, event mcp.ToolCallStartEvent) {
		s.sessionTracker.RecordToolStart(event.SessionID, event.ToolName)
		s.agentHub.Publish(map[string]any{
			"type":       "tool_start",
			"session_id": event.SessionID,
			"profile":    profileName,
			"tool_name":  event.ToolName,
			"api_name":   event.APIName,
			"timestamp":  time.Now(),
		})
	})

	// Fire after tool execution — record stats, audit, metrics
	mcpServer.SetToolCallHook(func(ctx context.Context, event mcp.ToolCallEvent) {
		s.sessionTracker.RecordToolEnd(event.SessionID, event.ToolName, event.Success, event.RequestSize, event.ResponseSize)
		s.agentHub.Publish(map[string]any{
			"type":          "tool_end",
			"session_id":    event.SessionID,
			"profile":       profileName,
			"tool_name":     event.ToolName,
			"success":       event.Success,
			"duration_ms":   event.Duration.Milliseconds(),
			"response_size": event.ResponseSize,
			"timestamp":     time.Now(),
		})
		s.auditLogger.LogExecute(ctx, profileName, event.APIName, event.ToolName, event.Arguments,
			event.Duration, 0, event.Success, event.ErrorMsg, "mcp", event.RequestSize, event.ResponseSize)
		s.metrics.RecordRequest(profileName, event.ToolName, event.Duration, event.Success)
	})

	// Auth config: use the profile's bearer token
	var authCfg *config.AuthConfig
	if s.authMode == "bearer" && prof.Token != "" {
		authCfg = &config.AuthConfig{
			Type:  "bearer",
			Token: prof.Token,
		}
	}

	// Create StreamableHTTPServer
	streamable := mcp.NewStreamableHTTPServer(mcpServer, s.logger, authCfg)

	// Track MCP session lifecycle for active connection metrics + agent monitoring
	streamable.SetSessionHook(func(event mcp.SessionEvent) {
		event.Profile = profileName
		s.logger.Printf("[MCP] session %s: %s (profile=%s, client=%v)", event.SessionID, event.Type, profileName, event.ClientInfo)
		if event.Type == "connected" {
			s.sessionTracker.Register(event.SessionID, profileName, event.ClientInfo)
			s.metrics.RecordConnection(true)
		} else {
			s.sessionTracker.Unregister(event.SessionID)
			s.metrics.RecordConnection(false)
		}
		s.agentHub.Publish(map[string]any{
			"type":        "session_" + event.Type,
			"session_id":  event.SessionID,
			"profile":     profileName,
			"client_info": event.ClientInfo,
			"timestamp":   time.Now(),
		})
	})

	// Cache it (evict old entries for this profile with different hashes)
	s.mcpServers.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prof.Name+":") && k != cacheKey {
			s.mcpServers.Delete(k)
		}
		return true
	})
	s.mcpServers.Store(cacheKey, streamable)

	s.logger.Printf("created MCP Streamable HTTP server for profile=%s", prof.Name)
	return streamable, nil
}
