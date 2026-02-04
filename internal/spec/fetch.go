package spec

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"mcp-api-bridge/internal/config"
)

type Fetcher struct {
	client *http.Client
}

func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{client: &http.Client{Timeout: timeout}}
}

func (f *Fetcher) Fetch(ctx context.Context, url string, auth *config.AuthConfig) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json, application/yaml, text/yaml, application/xml, text/xml, */*")
	applyAuth(req, auth)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch spec: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read spec: %w", err)
	}
	return data, nil
}

func (f *Fetcher) FetchGraphQLIntrospection(ctx context.Context, url string, auth *config.AuthConfig) ([]byte, error) {
	payload := map[string]string{"query": graphqlIntrospectionQuery}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("build introspection payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build introspection request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyAuth(req, auth)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch introspection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch introspection: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read introspection: %w", err)
	}
	return data, nil
}

func applyAuth(req *http.Request, auth *config.AuthConfig) {
	if auth == nil {
		return
	}
	switch auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "basic":
		cred := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))
		req.Header.Set("Authorization", "Basic "+cred)
	case "api-key":
		req.Header.Set(auth.Header, auth.Value)
	}
}
