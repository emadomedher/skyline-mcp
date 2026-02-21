package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestVerifyPKCE_S256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	if !VerifyPKCE(verifier, challenge, "S256") {
		t.Error("expected PKCE S256 verification to succeed")
	}
}

func TestVerifyPKCE_RejectsPlain(t *testing.T) {
	if VerifyPKCE("verifier", "verifier", "plain") {
		t.Error("expected PKCE plain to be rejected")
	}
}

func TestVerifyPKCE_RejectsEmpty(t *testing.T) {
	if VerifyPKCE("", "challenge", "S256") {
		t.Error("expected empty verifier to fail")
	}
	if VerifyPKCE("verifier", "", "S256") {
		t.Error("expected empty challenge to fail")
	}
}

func TestStoreClientRegistration(t *testing.T) {
	s := NewStore()
	client := s.RegisterClient("test-app", []string{"https://example.com/callback"})

	if client.ID == "" || client.Secret == "" {
		t.Error("expected non-empty client credentials")
	}
	if client.Name != "test-app" {
		t.Errorf("expected name test-app, got %s", client.Name)
	}

	// Retrieve
	got := s.GetClient(client.ID)
	if got == nil || got.ID != client.ID {
		t.Error("expected to find registered client")
	}

	// Validate secret
	if s.ValidateClientSecret(client.ID, client.Secret) == nil {
		t.Error("expected valid secret to pass")
	}
	if s.ValidateClientSecret(client.ID, "wrong-secret") != nil {
		t.Error("expected wrong secret to fail")
	}
}

func TestStoreRedirectURIValidation(t *testing.T) {
	s := NewStore()
	client := s.RegisterClient("test-app", []string{"https://example.com/callback"})

	if !client.ValidateRedirectURI("https://example.com/callback") {
		t.Error("expected registered redirect URI to be valid")
	}
	if client.ValidateRedirectURI("https://evil.com/callback") {
		t.Error("expected unregistered redirect URI to be invalid")
	}
}

func TestStoreAuthCodeFlow(t *testing.T) {
	s := NewStore()
	client := s.RegisterClient("test-app", []string{"https://example.com/callback"})

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	code := s.CreateAuthCode(client.ID, "https://example.com/callback", challenge, "S256", "my-profile", "profile-token-123")

	// Exchange with correct verifier
	token, err := s.ExchangeCode(code, client.ID, "https://example.com/callback", verifier)
	if err != nil {
		t.Fatalf("expected exchange to succeed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty access token")
	}

	// Code is single-use
	_, err = s.ExchangeCode(code, client.ID, "https://example.com/callback", verifier)
	if err == nil {
		t.Error("expected second exchange to fail")
	}
}

func TestStoreTokenValidation(t *testing.T) {
	s := NewStore()
	client := s.RegisterClient("test-app", []string{"https://example.com/callback"})

	verifier := "test-verifier-string"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	code := s.CreateAuthCode(client.ID, "https://example.com/callback", challenge, "S256", "my-profile", "profile-token-123")
	token, err := s.ExchangeCode(code, client.ID, "https://example.com/callback", verifier)
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}

	// Valid token
	at := s.ValidateToken(token)
	if at == nil {
		t.Fatal("expected valid token")
	}
	if at.ProfileName != "my-profile" {
		t.Errorf("expected profile name my-profile, got %s", at.ProfileName)
	}
	if at.ProfileToken != "profile-token-123" {
		t.Errorf("expected profile token profile-token-123, got %s", at.ProfileToken)
	}

	// Invalid token
	if s.ValidateToken("bogus-token") != nil {
		t.Error("expected invalid token to return nil")
	}
}

func TestStorePKCEMismatch(t *testing.T) {
	s := NewStore()
	client := s.RegisterClient("test-app", []string{"https://example.com/callback"})

	verifier := "correct-verifier"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	code := s.CreateAuthCode(client.ID, "https://example.com/callback", challenge, "S256", "my-profile", "tok")

	_, err := s.ExchangeCode(code, client.ID, "https://example.com/callback", "wrong-verifier")
	if err == nil {
		t.Error("expected PKCE mismatch to fail")
	}
}
