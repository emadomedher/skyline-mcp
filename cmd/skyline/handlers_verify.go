package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// handleVerify proxies credential verification requests to the target service.
// This is needed because browsers cannot call third-party APIs directly (CORS).
func (s *server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Service string `json:"service"`
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
		Email   string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "token is required"})
		return
	}

	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	switch req.Service {
	case "slack":
		s.verifySlack(w, r, client, req.Token)
	case "gitlab":
		s.verifyGitLab(w, r, client, req.BaseURL, req.Token)
	case "jira":
		s.verifyJira(w, r, client, req.BaseURL, req.Email, req.Token)
	default:
		http.Error(w, "unsupported service", http.StatusBadRequest)
	}
}

func (s *server) verifySlack(w http.ResponseWriter, r *http.Request, client *http.Client, token string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://slack.com/api/auth.test", nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "internal error"})
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "could not reach Slack API"})
		return
	}
	defer resp.Body.Close()

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&slackResp); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid response from Slack"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": slackResp.OK, "error": slackResp.Error})
}

func (s *server) verifyGitLab(w http.ResponseWriter, r *http.Request, client *http.Client, baseURL, token string) {
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/v4/user"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "internal error"})
		return
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "could not reach GitLab"})
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.StatusUnauthorized, http.StatusForbidden:
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "auth_error"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "unexpected response from GitLab"})
	}
}

func (s *server) verifyJira(w http.ResponseWriter, r *http.Request, client *http.Client, baseURL, email, token string) {
	if baseURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "instance URL is required"})
		return
	}
	base := strings.TrimRight(baseURL, "/")

	// Try v3 (Cloud) then v2 (Server/Data Center)
	for _, path := range []string{"/rest/api/3/myself", "/rest/api/2/myself"} {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, base+path, nil)
		if err != nil {
			continue
		}
		if email != "" {
			req.SetBasicAuth(email, token)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		case http.StatusUnauthorized, http.StatusForbidden:
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "auth_error"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "could not reach Jira instance"})
}
