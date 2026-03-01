package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// handleOAuthProtectedResource serves RFC 9728 Protected Resource Metadata.
// GET /.well-known/oauth-protected-resource
func (s *server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	issuer := "https://" + r.Host
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":              issuer,
		"authorization_servers": []string{issuer},
	})
}

// handleOAuthAuthorizationServer serves RFC 8414 Authorization Server Metadata.
// GET /.well-known/oauth-authorization-server
func (s *server) handleOAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	issuer := "https://" + r.Host
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"registration_endpoint":                 issuer + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
	})
}

// handleOAuthRegister implements RFC 7591 Dynamic Client Registration.
// POST /oauth/register
func (s *server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limitBody(w, r)
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "redirect_uris required"})
		return
	}

	client := s.oauthStore.RegisterClient(req.ClientName, req.RedirectURIs)
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ID,
		"client_secret":              client.Secret,
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "client_secret_post",
	})
}

// handleOAuthAuthorize handles both GET (show consent) and POST (submit consent).
// GET/POST /oauth/authorize
func (s *server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleOAuthAuthorizeGet(w, r)
	case http.MethodPost:
		s.handleOAuthAuthorizePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *server) handleOAuthAuthorizeGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	state := q.Get("state")

	// Validate required params
	if responseType != "code" {
		http.Error(w, "unsupported_response_type: only 'code' is supported", http.StatusBadRequest)
		return
	}
	if clientID == "" {
		http.Error(w, "client_id required", http.StatusBadRequest)
		return
	}

	client := s.oauthStore.GetClient(clientID)
	if client == nil {
		http.Error(w, "invalid client_id", http.StatusBadRequest)
		return
	}
	if !client.ValidateRedirectURI(redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		http.Error(w, "PKCE S256 code_challenge required", http.StatusBadRequest)
		return
	}

	// Render consent page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(renderConsentPage(client.Name, clientID, redirectURI, codeChallenge, codeChallengeMethod, state)))
}

func (s *server) handleOAuthAuthorizePost(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	// Read OAuth params from hidden fields
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	state := r.FormValue("state")

	// Read user credentials
	profileName := strings.TrimSpace(r.FormValue("profile_name"))
	profileToken := strings.TrimSpace(r.FormValue("profile_token"))
	action := r.FormValue("action")

	// Handle deny
	if action == "deny" {
		redirectWithError(w, r, redirectURI, state, "access_denied", "user denied the request")
		return
	}

	// Validate client
	client := s.oauthStore.GetClient(clientID)
	if client == nil {
		http.Error(w, "invalid client_id", http.StatusBadRequest)
		return
	}
	if !client.ValidateRedirectURI(redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}

	// Validate profile credentials
	if profileName == "" || profileToken == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(renderConsentPageWithError(
			client.Name, clientID, redirectURI, codeChallenge, codeChallengeMethod, state,
			"Profile name and token are required.",
		)))
		return
	}

	s.mu.RLock()
	prof, ok := s.findProfile(profileName)
	s.mu.RUnlock()
	if !ok || prof.Token != profileToken {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(renderConsentPageWithError(
			client.Name, clientID, redirectURI, codeChallenge, codeChallengeMethod, state,
			"Invalid profile name or token.",
		)))
		return
	}

	// Create authorization code
	code := s.oauthStore.CreateAuthCode(clientID, redirectURI, codeChallenge, codeChallengeMethod, profileName, profileToken)

	// Redirect back with code
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// handleOAuthToken exchanges an authorization code for an access token.
// POST /oauth/token
func (s *server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limitBody(w, r)

	// Support both form-encoded and JSON
	contentType := r.Header.Get("Content-Type")
	var (
		grantType    string
		code         string
		clientID     string
		clientSecret string
		codeVerifier string
		redirectURI  string
	)

	if strings.Contains(contentType, "application/json") {
		var req struct {
			GrantType    string `json:"grant_type"`
			Code         string `json:"code"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			CodeVerifier string `json:"code_verifier"`
			RedirectURI  string `json:"redirect_uri"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeOAuthError(w, "invalid_request", "invalid JSON body")
			return
		}
		grantType = req.GrantType
		code = req.Code
		clientID = req.ClientID
		clientSecret = req.ClientSecret
		codeVerifier = req.CodeVerifier
		redirectURI = req.RedirectURI
	} else {
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, "invalid_request", "invalid form body")
			return
		}
		grantType = r.FormValue("grant_type")
		code = r.FormValue("code")
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
		codeVerifier = r.FormValue("code_verifier")
		redirectURI = r.FormValue("redirect_uri")
	}

	if grantType != "authorization_code" {
		writeOAuthError(w, "unsupported_grant_type", "only authorization_code is supported")
		return
	}

	// Validate client credentials
	if s.oauthStore.ValidateClientSecret(clientID, clientSecret) == nil {
		writeOAuthError(w, "invalid_client", "invalid client credentials")
		return
	}

	// Exchange code for token
	accessToken, err := s.oauthStore.ExchangeCode(code, clientID, redirectURI, codeVerifier)
	if err != nil {
		writeOAuthError(w, "invalid_grant", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

func writeOAuthError(w http.ResponseWriter, errCode, description string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error":             errCode,
		"error_description": description,
	})
}

func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, state, errCode, errDesc string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", errCode)
	q.Set("error_description", errDesc)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
