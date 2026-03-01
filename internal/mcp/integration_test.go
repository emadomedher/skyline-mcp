package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"skyline-mcp/internal/config"
	"skyline-mcp/internal/logging"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
	"skyline-mcp/internal/spec"
)

func TestServerListAndCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testOpenAPI))
	})
	mux.HandleFunc("/echo/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/echo/")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := &config.Config{
		APIs: []config.APIConfig{
			{
				Name:            "test",
				SpecURL:         server.URL + "/openapi.json",
				BaseURLOverride: server.URL,
			},
		},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config validation failed: %v", err)
	}

	logger := logging.Discard()
	redactor := redact.NewRedactor()
	services, err := spec.LoadServices(context.Background(), cfg, logger, redactor)
	if err != nil {
		t.Fatalf("spec load failed: %v", err)
	}
	executor, err := runtime.NewExecutor(cfg, services, logger, redactor)
	if err != nil {
		t.Fatalf("executor init failed: %v", err)
	}
	registry, err := NewRegistry(services)
	if err != nil {
		t.Fatalf("registry init failed: %v", err)
	}

	mcpServer := NewServer(registry, executor, logger, redactor, "test")
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = mcpServer.Serve(ctx, inReader, outWriter)
		_ = outWriter.Close()
	}()

	dec := json.NewDecoder(outReader)
	send := func(payload any) {
		data, _ := json.Marshal(payload)
		_, _ = inWriter.Write(append(data, '\n'))
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	var listResp map[string]any
	if err := dec.Decode(&listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	result := listResp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("expected at least one tool")
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "test__echo",
			"arguments": map[string]any{
				"id": "42",
			},
		},
	})
	var callResp map[string]any
	if err := dec.Decode(&callResp); err != nil {
		t.Fatalf("decode call response: %v", err)
	}
	callResult := callResp["result"].(map[string]any)
	content := callResult["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected single content item")
	}
	contentItem := content[0].(map[string]any)
	text := contentItem["text"].(string)
	var jsonObj map[string]any
	if err := json.Unmarshal([]byte(text), &jsonObj); err != nil {
		t.Fatalf("failed to decode tool response: %v", err)
	}
	body := jsonObj["body"].(map[string]any)
	if body["id"] != "42" {
		t.Fatalf("unexpected body: %v", body)
	}

	_ = inWriter.Close()
}

const testOpenAPI = `{
  "openapi": "3.0.0",
  "info": {"title": "Echo", "version": "1.0"},
  "paths": {
    "/echo/{id}": {
      "get": {
        "operationId": "echo",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "ok",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {"id": {"type": "string"}}
                }
              }
            }
          }
        }
      }
    }
  }
}
`
