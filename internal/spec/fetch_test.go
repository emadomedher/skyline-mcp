package spec

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcp-api-bridge/internal/config"
)

func TestFetchWithBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
		if r.Header.Get("Authorization") != expected {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	fetcher := NewFetcher(2 * time.Second)
	auth := &config.AuthConfig{Type: "basic", Username: "user", Password: "pass"}
	data, err := fetcher.Fetch(context.Background(), server.URL, auth)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", string(data))
	}
}
