package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Store holds OAuth clients, authorization codes, and access tokens in memory.
type Store struct {
	mu      sync.RWMutex
	clients map[string]*Client      // client_id → Client
	codes   map[string]*AuthCode    // code → AuthCode
	tokens  map[string]*AccessToken // SHA-256(token) → AccessToken
}

// Client represents a registered OAuth client (e.g. ChatGPT).
type Client struct {
	ID           string   `json:"client_id"`
	Secret       string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
	Name         string   `json:"client_name"`
	CreatedAt    time.Time
}

// AuthCode represents a pending authorization code.
type AuthCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	ProfileName         string
	ProfileToken        string // the profile's bearer token
	ExpiresAt           time.Time
}

// AccessToken represents an issued OAuth access token.
type AccessToken struct {
	TokenHash    string // SHA-256 hex of the raw token
	ClientID     string
	ProfileName  string
	ProfileToken string // maps back to the profile's bearer token
	ExpiresAt    time.Time
}

const (
	codeExpiry  = 10 * time.Minute
	tokenExpiry = 1 * time.Hour
)

// NewStore creates a new OAuth store and starts a background cleanup goroutine.
func NewStore() *Store {
	s := &Store{
		clients: make(map[string]*Client),
		codes:   make(map[string]*AuthCode),
		tokens:  make(map[string]*AccessToken),
	}
	go s.cleanupLoop()
	return s
}

// RegisterClient creates a new OAuth client with generated credentials.
func (s *Store) RegisterClient(name string, redirectURIs []string) *Client {
	s.mu.Lock()
	defer s.mu.Unlock()

	client := &Client{
		ID:           generateRandomString(16),
		Secret:       generateRandomString(32),
		RedirectURIs: redirectURIs,
		Name:         name,
		CreatedAt:    time.Now(),
	}
	s.clients[client.ID] = client
	return client
}

// GetClient returns a client by ID, or nil if not found.
func (s *Store) GetClient(clientID string) *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[clientID]
}

// ValidateClientSecret checks that the client exists and the secret matches.
func (s *Store) ValidateClientSecret(clientID, clientSecret string) *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.clients[clientID]
	if c == nil || c.Secret != clientSecret {
		return nil
	}
	return c
}

// CreateAuthCode creates a new authorization code bound to a profile and PKCE challenge.
func (s *Store) CreateAuthCode(clientID, redirectURI, codeChallenge, codeChallengeMethod, profileName, profileToken string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	code := generateRandomString(32)
	s.codes[code] = &AuthCode{
		Code:                code,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ProfileName:         profileName,
		ProfileToken:        profileToken,
		ExpiresAt:           time.Now().Add(codeExpiry),
	}
	return code
}

// ExchangeCode consumes an authorization code and returns an access token string.
// Returns empty string and error if validation fails.
func (s *Store) ExchangeCode(code, clientID, redirectURI, codeVerifier string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ac := s.codes[code]
	if ac == nil {
		return "", fmt.Errorf("invalid authorization code")
	}
	delete(s.codes, code) // single-use

	if time.Now().After(ac.ExpiresAt) {
		return "", fmt.Errorf("authorization code expired")
	}
	if ac.ClientID != clientID {
		return "", fmt.Errorf("client_id mismatch")
	}
	if ac.RedirectURI != redirectURI {
		return "", fmt.Errorf("redirect_uri mismatch")
	}

	// Verify PKCE
	if !VerifyPKCE(codeVerifier, ac.CodeChallenge, ac.CodeChallengeMethod) {
		return "", fmt.Errorf("PKCE verification failed")
	}

	// Issue access token
	rawToken := generateRandomString(32)
	tokenHash := hashToken(rawToken)

	s.tokens[tokenHash] = &AccessToken{
		TokenHash:    tokenHash,
		ClientID:     clientID,
		ProfileName:  ac.ProfileName,
		ProfileToken: ac.ProfileToken,
		ExpiresAt:    time.Now().Add(tokenExpiry),
	}

	return rawToken, nil
}

// ValidateToken checks a raw bearer token and returns the associated AccessToken, or nil.
func (s *Store) ValidateToken(rawToken string) *AccessToken {
	s.mu.RLock()
	defer s.mu.RUnlock()

	h := hashToken(rawToken)
	at := s.tokens[h]
	if at == nil || time.Now().After(at.ExpiresAt) {
		return nil
	}
	return at
}

// ValidateRedirectURI checks if the given URI is registered for the client.
func (c *Client) ValidateRedirectURI(uri string) bool {
	for _, allowed := range c.RedirectURIs {
		if allowed == uri {
			return true
		}
	}
	return false
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, ac := range s.codes {
			if now.After(ac.ExpiresAt) {
				delete(s.codes, k)
			}
		}
		for k, at := range s.tokens {
			if now.After(at.ExpiresAt) {
				delete(s.tokens, k)
			}
		}
		s.mu.Unlock()
	}
}

func generateRandomString(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
