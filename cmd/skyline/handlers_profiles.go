package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"skyline-mcp/internal/config"
)

func (s *server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		names := make([]string, 0, len(s.store.Profiles))
		for _, p := range s.store.Profiles {
			if p.Name != "" {
				names = append(names, p.Name)
			}
		}
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"profiles": names})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleProfileOrGateway(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/tools") {
		s.handleProfileTools(w, r)
		return
	}
	if strings.HasSuffix(path, "/execute") {
		s.handleProfileExecute(w, r)
		return
	}
	if strings.HasSuffix(path, "/gateway") {
		s.handleGatewayWebSocket(w, r)
		return
	}
	s.handleProfile(w, r)
}

func (s *server) handleProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/profiles/")
	name = strings.TrimSpace(name)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		prof, ok := s.findProfile(name)
		s.mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := s.authorizeProfile(r, prof); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		if strings.EqualFold(r.URL.Query().Get("format"), "json") {
			var cfg config.Config
			if err := yaml.Unmarshal([]byte(prof.ConfigYAML), &cfg); err != nil {
				http.Error(w, "invalid stored config", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"name":   prof.Name,
				"token":  prof.Token,
				"config": cfg,
			})
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		_, _ = w.Write([]byte(prof.ConfigYAML))
	case http.MethodPut:
		var req upsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		req.Token = strings.TrimSpace(req.Token)
		req.ConfigYAML = strings.TrimSpace(req.ConfigYAML)
		if len(req.ConfigJSON) > 0 {
			var cfg config.Config
			if err := json.Unmarshal(req.ConfigJSON, &cfg); err != nil {
				http.Error(w, "invalid config_json", http.StatusBadRequest)
				return
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				http.Error(w, "failed to marshal config_json", http.StatusInternalServerError)
				return
			}
			req.ConfigYAML = strings.TrimSpace(string(data))
		}
		if req.ConfigYAML == "" {
			http.Error(w, "config_yaml or config_json is required", http.StatusBadRequest)
			return
		}
		if err := config.ValidateYAML([]byte(req.ConfigYAML)); err != nil {
			http.Error(w, fmt.Sprintf("invalid config_yaml: %v", err), http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		existing, ok := s.findProfile(name)
		if s.authMode == "bearer" {
			token := bearerToken(r.Header.Get("Authorization"))
			if ok {
				if token == "" || token != existing.Token {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			} else {
				if token == "" || token != req.Token {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
		if req.Token == "" {
			if ok {
				req.Token = existing.Token
			} else {
				http.Error(w, "token is required", http.StatusBadRequest)
				return
			}
		}
		if ok {
			existing.Token = req.Token
			existing.ConfigYAML = req.ConfigYAML
			s.updateProfile(existing)
		} else {
			s.store.Profiles = append(s.store.Profiles, profile{
				Name:       name,
				Token:      req.Token,
				ConfigYAML: req.ConfigYAML,
			})
		}
		if err := s.save(); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		if s.cache != nil {
			s.cache.evict(name)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		prof, ok := s.findProfile(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := s.authorizeProfile(r, prof); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		s.deleteProfile(name)
		if err := s.save(); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		if s.cache != nil {
			s.cache.evict(name)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) authorizeProfile(r *http.Request, prof profile) error {
	if s.authMode != "bearer" {
		return nil
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" || token != prof.Token {
		return fmt.Errorf("unauthorized")
	}
	return nil
}

func (s *server) handleProfileTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract profile name from URL path
	name := extractProfileName(r.URL.Path, "/profiles/", "/tools")
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}

	// Authenticate request
	s.mu.RLock()
	prof, ok := s.findProfile(name)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.authorizeProfile(r, prof); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cached, _, err := s.getOrBuildCache(ctx, prof)
	if err != nil {
		http.Error(w, fmt.Sprintf("load services: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert registry tools to response format
	tools := make([]toolInfo, 0, len(cached.registry.Tools))
	for _, tool := range cached.registry.Tools {
		tools = append(tools, toolInfo{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *server) handleProfileExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract profile name from URL path
	name := extractProfileName(r.URL.Path, "/profiles/", "/execute")
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}

	// Authenticate request
	s.mu.RLock()
	prof, ok := s.findProfile(name)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.authorizeProfile(r, prof); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse request
	var req executeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if req.ToolName == "" {
		http.Error(w, "tool_name is required", http.StatusBadRequest)
		return
	}

	startTime := time.Now()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cached, _, err := s.getOrBuildCache(ctx, prof)
	if err != nil {
		s.metrics.RecordRequest(name, req.ToolName, time.Since(startTime), false)
		http.Error(w, fmt.Sprintf("load services: %v", err), http.StatusInternalServerError)
		return
	}

	// Look up the tool by name
	tool, ok := cached.registry.Tools[req.ToolName]
	if !ok {
		s.metrics.RecordRequest(name, req.ToolName, time.Since(startTime), false)
		http.Error(w, fmt.Sprintf("unknown tool: %s", req.ToolName), http.StatusNotFound)
		return
	}

	// Execute the operation
	result, err := cached.executor.Execute(ctx, tool.Operation, req.Arguments)
	duration := time.Since(startTime)
	if err != nil {
		s.metrics.RecordRequest(name, req.ToolName, duration, false)
		http.Error(w, fmt.Sprintf("execute: %v", err), http.StatusInternalServerError)
		return
	}

	s.metrics.RecordRequest(name, req.ToolName, duration, true)
	writeJSON(w, http.StatusOK, result)
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func extractProfileName(path, prefix, suffix string) string {
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, suffix)
	return strings.TrimSpace(path)
}
