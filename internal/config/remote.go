package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func FetchProfileConfig(ctx context.Context, baseURL, profile, token string) ([]byte, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("config url is required")
	}
	if strings.TrimSpace(profile) == "" {
		return nil, fmt.Errorf("profile is required")
	}
	baseURL = strings.TrimRight(baseURL, "/")
	u := baseURL + "/profiles/" + url.PathEscape(profile)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/yaml, application/yaml, text/plain, */*")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch profile: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch profile: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	return data, nil
}
