package googleapi_test

import (
	"context"
	"encoding/json"
	"testing"

	"mcp-api-bridge/internal/parsers/googleapi"
)

func TestLooksLikeDiscovery(t *testing.T) {
	raw := []byte(`{"kind":"discovery#restDescription","name":"demo"}`)
	if !googleapi.LooksLikeDiscovery(raw) {
		t.Fatalf("expected discovery doc detection")
	}
}

func TestParseToCanonical(t *testing.T) {
	doc := map[string]any{
		"kind":        "discovery#restDescription",
		"name":        "demo",
		"rootUrl":     "https://example.com/",
		"servicePath": "api/",
		"resources": map[string]any{
			"widgets": map[string]any{
				"methods": map[string]any{
					"list": map[string]any{
						"id":         "demo.widgets.list",
						"path":       "v1/widgets",
						"httpMethod": "GET",
						"description": "List widgets",
						"parameters": map[string]any{
							"pageSize": map[string]any{
								"location":    "query",
								"type":        "integer",
								"description": "Max items",
							},
						},
					},
					"get": map[string]any{
						"id":         "demo.widgets.get",
						"path":       "v1/widgets/{widgetId}",
						"httpMethod": "GET",
						"parameters": map[string]any{
							"widgetId": map[string]any{
								"location": "path",
								"type":     "string",
								"required": true,
							},
						},
						"response": map[string]any{"$ref": "Widget"},
					},
				},
			},
		},
		"schemas": map[string]any{
			"Widget": map[string]any{
				"id":   "Widget",
				"type": "object",
				"properties": map[string]any{
					"id":   map[string]any{"type": "string"},
					"name": map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
		},
	}
	raw, _ := json.Marshal(doc)
	service, err := googleapi.ParseToCanonical(context.Background(), raw, "demo-api", "")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if service.BaseURL != "https://example.com/api" {
		t.Fatalf("unexpected base URL: %s", service.BaseURL)
	}
	if len(service.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(service.Operations))
	}
	foundList := false
	for _, op := range service.Operations {
		if op.ID == "widgets.list" {
			foundList = true
			if _, ok := op.InputSchema["properties"].(map[string]any)["pageSize"]; !ok {
				t.Fatalf("expected pageSize parameter schema")
			}
		}
		if op.ID == "widgets.get" {
			if req, ok := op.InputSchema["required"].([]string); ok {
				hasID := false
				for _, v := range req {
					if v == "widgetId" {
						hasID = true
					}
				}
				if !hasID {
					t.Fatalf("expected widgetId required")
				}
			}
		}
	}
	if !foundList {
		t.Fatalf("missing widgets.list operation")
	}
}
