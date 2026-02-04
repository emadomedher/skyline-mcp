package postman

import (
	"context"
	"testing"
)

const minimalCollection = `{
  "info": {
    "name": "Pet Store",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "variable": [
    { "key": "baseUrl", "value": "http://localhost:3000" }
  ],
  "item": [
    {
      "name": "Pets",
      "item": [
        {
          "name": "List Pets",
          "request": {
            "method": "GET",
            "url": {
              "raw": "{{baseUrl}}/pets",
              "host": ["{{baseUrl}}"],
              "path": ["pets"],
              "query": [
                { "key": "limit", "value": "10", "description": "Max items to return" },
                { "key": "status", "value": "available", "description": "Filter by status" }
              ]
            }
          }
        },
        {
          "name": "Get Pet",
          "request": {
            "method": "GET",
            "url": {
              "raw": "{{baseUrl}}/pets/:petId",
              "host": ["{{baseUrl}}"],
              "path": ["pets", ":petId"],
              "variable": [
                { "key": "petId", "value": "1" }
              ]
            }
          }
        },
        {
          "name": "Create Pet",
          "request": {
            "method": "POST",
            "url": {
              "raw": "{{baseUrl}}/pets",
              "host": ["{{baseUrl}}"],
              "path": ["pets"]
            },
            "header": [
              { "key": "X-Request-Id", "value": "abc123" }
            ],
            "body": {
              "mode": "raw",
              "raw": "{\"name\": \"Fido\", \"tag\": \"dog\"}",
              "options": { "raw": { "language": "json" } }
            },
            "description": "Create a new pet"
          }
        },
        {
          "name": "Delete Pet",
          "request": {
            "method": "DELETE",
            "url": "{{baseUrl}}/pets/{{petId}}"
          }
        }
      ]
    }
  ]
}`

func TestLooksLikePostmanCollection(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"v2.1 schema", `{"info":{"schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}}`, true},
		{"v2.0 schema", `{"info":{"schema":"https://schema.getpostman.com/json/collection/v2.0.0/collection.json"}}`, true},
		{"not postman", `{"openapi":"3.0.0"}`, false},
		{"empty", `{}`, false},
		{"invalid json", `not json`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikePostmanCollection([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikePostmanCollection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalCollection), "petstore", "")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "petstore" {
		t.Errorf("Name = %q, want %q", svc.Name, "petstore")
	}
	if svc.BaseURL != "http://localhost:3000" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "http://localhost:3000")
	}

	if len(svc.Operations) != 4 {
		t.Fatalf("len(Operations) = %d, want 4", len(svc.Operations))
	}

	// Operations are sorted by ToolName.
	ops := make(map[string]struct{ Method, Path string })
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, Path string }{op.Method, op.Path}
	}

	// Check List Pets
	if op, ok := ops["Pets_List_Pets"]; !ok {
		t.Error("missing Pets_List_Pets operation")
	} else {
		if op.Method != "get" {
			t.Errorf("List_Pets method = %q, want %q", op.Method, "get")
		}
		if op.Path != "/pets" {
			t.Errorf("List_Pets path = %q, want %q", op.Path, "/pets")
		}
	}

	// Check Get Pet has path param
	if op, ok := ops["Pets_Get_Pet"]; !ok {
		t.Error("missing Pets_Get_Pet operation")
	} else {
		if op.Path != "/pets/{petId}" {
			t.Errorf("Get_Pet path = %q, want %q", op.Path, "/pets/{petId}")
		}
	}

	// Check Create Pet has body and header
	found := false
	for _, op := range svc.Operations {
		if op.ID == "Pets_Create_Pet" {
			found = true
			if op.Method != "post" {
				t.Errorf("Create_Pet method = %q, want %q", op.Method, "post")
			}
			if op.RequestBody == nil {
				t.Error("Create_Pet has no request body")
			} else if op.RequestBody.ContentType != "application/json" {
				t.Errorf("Create_Pet content type = %q, want %q", op.RequestBody.ContentType, "application/json")
			}
			hasHeader := false
			for _, p := range op.Parameters {
				if p.In == "header" && p.Name == "X-Request-Id" {
					hasHeader = true
				}
			}
			if !hasHeader {
				t.Error("Create_Pet missing X-Request-Id header param")
			}
		}
	}
	if !found {
		t.Error("missing Pets_Create_Pet operation")
	}

	// Check Delete Pet (string URL form)
	if op, ok := ops["Pets_Delete_Pet"]; !ok {
		t.Error("missing Pets_Delete_Pet operation")
	} else {
		if op.Method != "delete" {
			t.Errorf("Delete_Pet method = %q, want %q", op.Method, "delete")
		}
	}
}

func TestParseToCanonical_BaseURLOverride(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalCollection), "test", "https://api.example.com")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}
	if svc.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://api.example.com")
	}
}

func TestParseToCanonical_NoBaseURL(t *testing.T) {
	noVar := `{
		"info": {
			"name": "Test",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
		},
		"item": [
			{
				"name": "Ping",
				"request": { "method": "GET", "url": "http://localhost/ping" }
			}
		]
	}`
	_, err := ParseToCanonical(context.Background(), []byte(noVar), "test", "")
	if err == nil {
		t.Error("expected error when no base URL")
	}
}

func TestParseToCanonical_FormData(t *testing.T) {
	col := `{
		"info": {
			"name": "Upload",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
		},
		"variable": [{ "key": "baseUrl", "value": "http://localhost" }],
		"item": [
			{
				"name": "Upload File",
				"request": {
					"method": "POST",
					"url": { "raw": "{{baseUrl}}/upload", "host": ["{{baseUrl}}"], "path": ["upload"] },
					"body": { "mode": "formdata", "raw": "" }
				}
			}
		]
	}`
	svc, err := ParseToCanonical(context.Background(), []byte(col), "upload", "")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}
	if len(svc.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(svc.Operations))
	}
	if svc.Operations[0].RequestBody == nil {
		t.Fatal("expected request body")
	}
	if svc.Operations[0].RequestBody.ContentType != "multipart/form-data" {
		t.Errorf("content type = %q, want %q", svc.Operations[0].RequestBody.ContentType, "multipart/form-data")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"List Pets", "List_Pets"},
		{"get-by-id", "get_by_id"},
		{"folder/sub", "folder_sub"},
		{"special!@#chars", "specialchars"},
		{"already_ok", "already_ok"},
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
