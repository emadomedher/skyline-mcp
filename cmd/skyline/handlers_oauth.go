package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	googleAuthEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenEndpoint = "https://oauth2.googleapis.com/token"
)

// handleOAuthStart generates a Google OAuth consent URL for the frontend to open.
func (s *server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ClientID    string `json:"client_id"`
		RedirectURI string `json:"redirect_uri"`
		Scopes      string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.ClientID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "client_id is required"})
		return
	}
	if req.Scopes == "" {
		req.Scopes = "https://www.googleapis.com/auth/gmail.modify"
	}
	if req.RedirectURI == "" {
		req.RedirectURI = fmt.Sprintf("https://%s/oauth/callback", r.Host)
	}

	params := url.Values{
		"client_id":     {req.ClientID},
		"redirect_uri":  {req.RedirectURI},
		"response_type": {"code"},
		"scope":         {req.Scopes},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
	}

	authURL := googleAuthEndpoint + "?" + params.Encode()
	writeJSON(w, http.StatusOK, map[string]any{"auth_url": authURL, "redirect_uri": req.RedirectURI})
}

// handleOAuthCallback receives the authorization code from Google's redirect
// and serves an HTML page that posts the result to the parent window.
func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	errorParam := r.URL.Query().Get("error")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if errorParam != "" {
		w.Write([]byte(oauthCallbackPage("Authorization failed: "+errorParam, "false", "", errorParam)))
		return
	}
	if code == "" {
		w.Write([]byte(oauthCallbackPage("No authorization code received.", "false", "", "no code")))
		return
	}

	w.Write([]byte(oauthCallbackPage("Authorization successful. This window will close automatically.", "true", code, "")))
}

// handleOAuthExchange exchanges an authorization code for access + refresh tokens.
func (s *server) handleOAuthExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Code         string `json:"code"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Code == "" || req.ClientID == "" || req.ClientSecret == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "code, client_id, and client_secret are required"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	data := url.Values{
		"code":          {req.Code},
		"client_id":     {req.ClientID},
		"client_secret": {req.ClientSecret},
		"redirect_uri":  {req.RedirectURI},
		"grant_type":    {"authorization_code"},
	}

	resp, err := client.PostForm(googleTokenEndpoint, data)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "token exchange failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "failed to parse token response"})
		return
	}
	if tokenResp.Error != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": tokenResp.Error + ": " + tokenResp.ErrorDesc})
		return
	}
	if tokenResp.RefreshToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    false,
			"error": "no refresh_token returned — revoke app access at myaccount.google.com/permissions and retry",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"refresh_token": tokenResp.RefreshToken,
		"access_token":  tokenResp.AccessToken,
		"scope":         tokenResp.Scope,
	})
}

func oauthCallbackPage(message, successJS, code, errMsg string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Skyline OAuth</title>
<style>body{font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0;}</style>
</head><body>
<p>%s</p>
<script>
if (window.opener) {
  window.opener.postMessage({
    type: 'skyline-oauth-callback',
    success: %s,
    code: %q,
    error: %q
  }, window.location.origin);
}
setTimeout(function() { window.close(); }, 2000);
</script>
</body></html>`, message, successJS, code, errMsg)
}
