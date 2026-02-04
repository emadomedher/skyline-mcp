package openapi

import (
	"context"
	"testing"

	"mcp-api-bridge/internal/canonical"
)

func TestParseToCanonicalParameters(t *testing.T) {
	spec := []byte(`{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "paths": {
    "/items/{id}": {
      "get": {
        "operationId": "getItem",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "q", "in": "query", "schema": {"type": "string"}},
          {"name": "X-Trace", "in": "header", "schema": {"type": "string"}},
          {"name": "Authorization", "in": "header", "schema": {"type": "string"}}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    },
    "/items": {
      "post": {
        "operationId": "createItem",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"type": "object", "properties": {"name": {"type": "string"}}}
            }
          }
        },
        "responses": {"201": {"description": "created"}}
      }
    }
  }
}`)

	service, err := ParseToCanonical(context.Background(), spec, "test", "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var getOp, postOp *canonical.Operation
	for _, op := range service.Operations {
		switch op.ToolName {
		case "test__getItem":
			getOp = op
		case "test__createItem":
			postOp = op
		}
	}
	if getOp == nil || postOp == nil {
		t.Fatalf("expected operations not found")
	}
	props := getOp.InputSchema["properties"].(map[string]any)
	if _, ok := props["id"]; !ok {
		t.Fatalf("expected path parameter id in schema")
	}
	if _, ok := props["q"]; !ok {
		t.Fatalf("expected query parameter q in schema")
	}
	if _, ok := props["X-Trace"]; !ok {
		t.Fatalf("expected header parameter X-Trace in schema")
	}
	if _, ok := props["Authorization"]; ok {
		t.Fatalf("expected Authorization header to be excluded")
	}

	req := getOp.InputSchema["required"].([]string)
	if len(req) != 1 || req[0] != "id" {
		t.Fatalf("expected only id to be required")
	}

	if postOp.RequestBody == nil {
		t.Fatalf("expected request body")
	}
	postProps := postOp.InputSchema["properties"].(map[string]any)
	if _, ok := postProps["body"]; !ok {
		t.Fatalf("expected body in input schema")
	}
}
