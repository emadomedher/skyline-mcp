package openrpc

import (
	"context"
	"testing"
)

const calculatorSpec = `{
  "openrpc": "1.2.6",
  "info": { "title": "Calculator", "version": "1.0.0" },
  "servers": [{ "name": "local", "url": "http://localhost:9999/jsonrpc" }],
  "methods": [
    {
      "name": "add",
      "summary": "Add two numbers",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    },
    {
      "name": "subtract",
      "summary": "Subtract b from a",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    },
    {
      "name": "multiply",
      "summary": "Multiply two numbers",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    },
    {
      "name": "divide",
      "summary": "Divide a by b",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    }
  ]
}`

func TestLooksLikeOpenRPC(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"valid openrpc", `{"openrpc":"1.2.6","info":{"title":"T","version":"1.0"}}`, true},
		{"openapi not openrpc", `{"openapi":"3.0.0"}`, false},
		{"empty", `{}`, false},
		{"invalid json", `not json`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeOpenRPC([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeOpenRPC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(calculatorSpec), "calc", "")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "calc" {
		t.Errorf("Name = %q, want %q", svc.Name, "calc")
	}
	if svc.BaseURL != "http://localhost:9999/jsonrpc" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "http://localhost:9999/jsonrpc")
	}
	if len(svc.Operations) != 4 {
		t.Fatalf("len(Operations) = %d, want 4", len(svc.Operations))
	}

	ops := map[string]bool{}
	for _, op := range svc.Operations {
		ops[op.ID] = true
		if op.JSONRPC == nil {
			t.Errorf("operation %s missing JSONRPC metadata", op.ID)
			continue
		}
		if op.Method != "post" {
			t.Errorf("operation %s method = %q, want %q", op.ID, op.Method, "post")
		}
		if op.Path != "/" {
			t.Errorf("operation %s path = %q, want %q", op.ID, op.Path, "/")
		}
		if len(op.Parameters) != 2 {
			t.Errorf("operation %s has %d params, want 2", op.ID, len(op.Parameters))
		}
	}

	for _, name := range []string{"add", "subtract", "multiply", "divide"} {
		if !ops[name] {
			t.Errorf("missing operation %s", name)
		}
	}

	// Verify JSONRPC method name mapping
	for _, op := range svc.Operations {
		if op.JSONRPC.MethodName != op.ID {
			t.Errorf("operation %s JSONRPC.MethodName = %q, want %q", op.ID, op.JSONRPC.MethodName, op.ID)
		}
	}
}

func TestParseToCanonical_BaseURLOverride(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(calculatorSpec), "calc", "https://api.example.com/rpc")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}
	if svc.BaseURL != "https://api.example.com/rpc" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://api.example.com/rpc")
	}
}

func TestParseToCanonical_NoBaseURL(t *testing.T) {
	noServer := `{
		"openrpc": "1.2.6",
		"info": { "title": "T", "version": "1.0" },
		"methods": [
			{
				"name": "ping",
				"params": [],
				"result": { "name": "result", "schema": { "type": "string" } }
			}
		]
	}`
	_, err := ParseToCanonical(context.Background(), []byte(noServer), "test", "")
	if err == nil {
		t.Error("expected error when no base URL")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"add", "add"},
		{"rpc.discover", "rpc_discover"},
		{"my-method", "my_method"},
		{"some method", "some_method"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
