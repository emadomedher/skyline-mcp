package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"skyline-mcp/internal/audit"
)

// isAdminSession returns true if the request carries a valid admin session cookie.
func (s *server) isAdminSession(r *http.Request) bool {
	cookie, err := r.Cookie("skyline_admin")
	if err != nil {
		return false
	}
	return cookie.Value == s.adminToken
}

// handleAdminAuth handles GET (check) and POST (login) for admin authentication.
func (s *server) handleAdminAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.isAdminSession(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case http.MethodPost:
		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Token != s.adminToken {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "skyline_admin",
			Value:    s.adminToken,
			Path:     "/",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   86400 * 7, // 7 days
		})
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMetrics returns Prometheus-compatible metrics
func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isAdminSession(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(s.metrics.PrometheusFormat()))
}

// handleAudit returns audit log entries
func (s *server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isAdminSession(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	profileName := query.Get("profile")
	eventType := query.Get("event_type")
	toolName := query.Get("tool_name")
	limit := 100
	if l := query.Get("limit"); l != "" {
		if parsed, err := fmt.Sscanf(l, "%d", &limit); err == nil && parsed == 1 {
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	// Query audit log
	events, err := s.auditLogger.Query(audit.QueryOptions{
		Profile:   profileName,
		EventType: eventType,
		ToolName:  toolName,
		Limit:     limit,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("query audit log: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

// handleStats returns aggregated statistics
func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isAdminSession(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	profileName := query.Get("profile")

	// Default to last 24 hours
	since := time.Now().Add(-24 * time.Hour)
	if sinceStr := query.Get("since"); sinceStr != "" {
		if parsed, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = parsed
		}
	}

	// Get audit stats
	auditStats, err := s.auditLogger.GetStats(profileName, since)
	if err != nil {
		http.Error(w, fmt.Sprintf("get stats: %v", err), http.StatusInternalServerError)
		return
	}

	// Get metrics snapshot
	metricsSnapshot := s.metrics.Snapshot()

	writeJSON(w, http.StatusOK, map[string]any{
		"audit_stats":      auditStats,
		"metrics_snapshot": metricsSnapshot,
		"period": map[string]any{
			"since": since,
			"until": time.Now(),
		},
	})
}

// handleSessions returns current active MCP sessions.
func (s *server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isAdminSession(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessions := s.sessionTracker.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// handleEventStream serves a Server-Sent Events stream of live audit + agent events.
func (s *server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isAdminSession(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Use ResponseController to override the server's WriteTimeout for this
	// long-lived connection.
	rc := http.NewResponseController(w)

	// Subscribe to the audit event hub
	auditHub := s.auditLogger.EventHub()
	auditSubID, auditCh := auditHub.Subscribe()
	defer auditHub.Unsubscribe(auditSubID)

	// Subscribe to the agent event hub
	agentSubID, agentCh := s.agentHub.Subscribe()
	defer s.agentHub.Unsubscribe(agentSubID)

	// Send connected event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-auditCh:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprintf(w, "event: audit\ndata: %s\n\n", data)
			flusher.Flush()
		case event, ok := <-agentCh:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprintf(w, "event: agent\ndata: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}

// handleConfig manages server configuration (config.yaml)
func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !s.isAdminSession(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w, r)
	case http.MethodPost:
		s.handlePostConfig(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetConfig returns the current server configuration
func (s *server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	// Read config file
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist - return empty default
			writeJSON(w, http.StatusOK, map[string]any{
				"raw": "# Skyline MCP Server Configuration\n# File not found - using defaults\n",
				"server": map[string]any{
					"listen": "localhost:8191",
				},
				"runtime": map[string]any{
					"codeExecution": map[string]any{
						"enabled": true,
					},
				},
				"audit": map[string]any{
					"enabled": true,
				},
				"logging": map[string]any{
					"level": "info",
				},
			})
			return
		}
		http.Error(w, fmt.Sprintf("read config: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse YAML to validate and provide structured response
	var configData map[string]any
	if err := yaml.Unmarshal(data, &configData); err != nil {
		http.Error(w, fmt.Sprintf("parse config: %v", err), http.StatusBadRequest)
		return
	}

	// Return both raw YAML and parsed structure
	response := map[string]any{
		"raw": string(data),
	}

	// Add parsed fields if they exist
	if srv, ok := configData["server"].(map[string]any); ok {
		response["server"] = srv
	}
	if rt, ok := configData["runtime"].(map[string]any); ok {
		response["runtime"] = rt
	}
	if aud, ok := configData["audit"].(map[string]any); ok {
		response["audit"] = aud
	}
	if profiles, ok := configData["profiles"].(map[string]any); ok {
		response["profiles"] = profiles
	}
	if security, ok := configData["security"].(map[string]any); ok {
		response["security"] = security
	}
	if logging, ok := configData["logging"].(map[string]any); ok {
		response["logging"] = logging
	}

	writeJSON(w, http.StatusOK, response)
}

// handlePostConfig saves updated server configuration
func (s *server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	// Read request body (raw YAML)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate YAML syntax
	var configData map[string]any
	if err := yaml.Unmarshal(data, &configData); err != nil {
		http.Error(w, fmt.Sprintf("invalid yaml: %v", err), http.StatusBadRequest)
		return
	}

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		http.Error(w, fmt.Sprintf("create config dir: %v", err), http.StatusInternalServerError)
		return
	}

	// Write config file atomically (write to temp, then rename)
	tmp := s.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		http.Error(w, fmt.Sprintf("write temp file: %v", err), http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tmp, s.configPath); err != nil {
		os.Remove(tmp) // Clean up temp file on error
		http.Error(w, fmt.Sprintf("save config: %v", err), http.StatusInternalServerError)
		return
	}

	s.logger.Info("config saved", "path", s.configPath)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": "Configuration saved successfully. Restart the server for changes to take effect.",
		"path":    s.configPath,
	})
}
