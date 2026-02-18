package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"skyline-mcp/internal/config"
	"skyline-mcp/internal/redact"
)

func TestSSEInitializeFlow(t *testing.T) {
	registry := &Registry{Tools: map[string]*Tool{}, Resources: map[string]*Resource{}}
	logger := log.New(io.Discard, "", 0)
	server := NewServer(registry, nil, logger, redact.NewRedactor(), "test")
	httpServer := NewHTTPServer(server, logger, &config.AuthConfig{Type: "bearer", Token: "dev-token"})

	ts := httptest.NewServer(httpServer.handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sse", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer dev-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse request failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	event, data := readSSEEvent(t, reader)
	if event != "endpoint" {
		t.Fatalf("expected endpoint event, got %q", event)
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("endpoint payload: %v", err)
	}
	if payload.URL == "" {
		t.Fatalf("missing endpoint url")
	}

	postReq, err := http.NewRequest(http.MethodPost, payload.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	postReq.Header.Set("Authorization", "Bearer dev-token")
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	_ = postResp.Body.Close()

	event, data = readSSEEvent(t, reader)
	if event != "message" {
		t.Fatalf("expected message event, got %q", event)
	}
	var rpc rpcResponse
	if err := json.Unmarshal(data, &rpc); err != nil {
		t.Fatalf("rpc decode: %v", err)
	}
	if rpc.Result == nil {
		t.Fatalf("expected initialize result")
	}
}

func TestSSEAuthRequired(t *testing.T) {
	registry := &Registry{Tools: map[string]*Tool{}, Resources: map[string]*Resource{}}
	logger := log.New(io.Discard, "", 0)
	server := NewServer(registry, nil, logger, redact.NewRedactor(), "test")
	httpServer := NewHTTPServer(server, logger, &config.AuthConfig{Type: "bearer", Token: "dev-token"})

	ts := httptest.NewServer(httpServer.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/sse")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) (string, []byte) {
	t.Helper()
	var event string
	var dataLines [][]byte
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))))
		}
	}
	data := []byte(strings.Join(stringSlice(dataLines), "\n"))
	return event, data
}

func stringSlice(lines [][]byte) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, string(line))
	}
	return out
}
