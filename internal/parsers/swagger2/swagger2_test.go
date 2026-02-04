package swagger2

import (
	"context"
	"testing"
)

func TestParseSwagger2ToCanonical(t *testing.T) {
	spec := []byte(`{
  "swagger": "2.0",
  "info": {"title": "Pets", "version": "1.0"},
  "host": "example.com",
  "basePath": "/v1",
  "schemes": ["https"],
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "parameters": [
          {"name": "limit", "in": "query", "type": "integer"}
        ],
        "responses": {
          "200": {"description": "ok"}
        }
      }
    }
  }
}`)

	service, err := ParseToCanonical(context.Background(), spec, "pets", "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if service.BaseURL != "https://example.com/v1" {
		t.Fatalf("unexpected base url: %s", service.BaseURL)
	}
	if len(service.Operations) != 1 {
		t.Fatalf("expected 1 operation")
	}
	op := service.Operations[0]
	if op.ToolName != "pets__listPets" {
		t.Fatalf("unexpected tool name: %s", op.ToolName)
	}
	props := op.InputSchema["properties"].(map[string]any)
	if _, ok := props["limit"]; !ok {
		t.Fatalf("expected query param limit")
	}
}
