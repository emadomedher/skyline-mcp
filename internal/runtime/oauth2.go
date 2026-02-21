package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"skyline-mcp/internal/config"
)

const (
	defaultGoogleTokenURL = "https://oauth2.googleapis.com/token"
	tokenExpiryBuffer     = 5 * time.Minute
)

// OAuth2TokenManager caches OAuth2 access tokens per API and refreshes
// them automatically when they expire. Thread-safe.
type OAuth2TokenManager struct {
	mu     sync.Mutex
	tokens map[string]*cachedToken
	client *http.Client
}

type cachedToken struct {
	accessToken string
	expiresAt   time.Time
}

// NewOAuth2TokenManager creates a new token manager.
func NewOAuth2TokenManager() *OAuth2TokenManager {
	return &OAuth2TokenManager{
		tokens: make(map[string]*cachedToken),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetAccessToken returns a valid access token for the given API,
// refreshing from the token endpoint if the cached token is expired.
func (m *OAuth2TokenManager) GetAccessToken(apiName string, auth *config.AuthConfig) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cached, ok := m.tokens[apiName]; ok {
		if time.Now().Before(cached.expiresAt.Add(-tokenExpiryBuffer)) {
			return cached.accessToken, nil
		}
	}

	tokenURL := auth.TokenURL
	if tokenURL == "" {
		tokenURL = defaultGoogleTokenURL
	}

	data := url.Values{
		"client_id":     {auth.ClientID},
		"client_secret": {auth.ClientSecret},
		"refresh_token": {auth.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := m.client.PostForm(tokenURL, data)
	if err != nil {
		return "", fmt.Errorf("oauth2 token refresh: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("oauth2 token parse: %w", err)
	}
	if tokenResp.Error != "" {
		return "", fmt.Errorf("oauth2: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("oauth2 token refresh: empty access_token")
	}

	expiresIn := time.Duration(tokenResp.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 3600 * time.Second
	}

	m.tokens[apiName] = &cachedToken{
		accessToken: tokenResp.AccessToken,
		expiresAt:   time.Now().Add(expiresIn),
	}

	return tokenResp.AccessToken, nil
}
