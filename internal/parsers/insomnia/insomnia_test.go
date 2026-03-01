package insomnia

import (
	"context"
	"testing"
)

const minimalExport = `{
  "_type": "export",
  "__export_format": 4,
  "resources": [
    {"_id": "env_1", "_type": "environment", "parentId": null, "name": "Base Environment", "data": {"base_url": "https://api.example.com"}},
    {"_id": "fld_1", "_type": "request_group", "parentId": null, "name": "Pets"},
    {"_id": "req_1", "_type": "request", "parentId": "fld_1", "name": "List Pets", "method": "GET", "url": "https://api.example.com/pets", "parameters": [{"name": "limit", "value": "10"}], "headers": [], "body": null},
    {"_id": "req_2", "_type": "request", "parentId": "fld_1", "name": "Create Pet", "method": "POST", "url": "https://api.example.com/pets", "parameters": [], "headers": [{"name": "X-Request-Id", "value": "abc"}], "body": {"mimeType": "application/json", "text": "{\"name\": \"Fido\"}"}}
  ]
}`

func TestLooksLikeInsomniaCollection(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"v4 export", `{"_type":"export","__export_format":4}`, true},
		{"v5 export", `{"_type":"export","__export_format":5}`, true},
		{"v3 export (too old)", `{"_type":"export","__export_format":3}`, false},
		{"wrong type", `{"_type":"workspace","__export_format":4}`, false},
		{"not json", "hello world", false},
		{"empty", "", false},
		{"openapi doc", `{"openapi":"3.0.0"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeInsomniaCollection([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeInsomniaCollection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalExport), "petapi", "")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "petapi" {
		t.Errorf("Name = %q, want %q", svc.Name, "petapi")
	}
	if svc.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://api.example.com")
	}

	if len(svc.Operations) != 2 {
		t.Fatalf("len(Operations) = %d, want 2", len(svc.Operations))
	}

	ops := map[string]struct{ Method, Path string }{}
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, Path string }{op.Method, op.Path}
	}

	// operationID = folderPrefix + "_" + sanitizeName(name)
	// folderPrefix = sanitizeName("Pets") = "Pets"
	// sanitizeName("List Pets") = "List_Pets"
	// So: "Pets_List_Pets"
	if op, ok := ops["Pets_List_Pets"]; !ok {
		t.Errorf("missing Pets_List_Pets; have %v", ops)
	} else {
		if op.Method != "get" {
			t.Errorf("List Pets method = %q, want %q", op.Method, "get")
		}
		if op.Path != "/pets" {
			t.Errorf("List Pets path = %q, want %q", op.Path, "/pets")
		}
	}

	if op, ok := ops["Pets_Create_Pet"]; !ok {
		t.Errorf("missing Pets_Create_Pet; have %v", ops)
	} else {
		if op.Method != "post" {
			t.Errorf("Create Pet method = %q, want %q", op.Method, "post")
		}
	}

	// Verify Create Pet has a request body and X-Request-Id header.
	for _, op := range svc.Operations {
		if op.ID == "Pets_Create_Pet" {
			if op.RequestBody == nil {
				t.Error("Create Pet should have a request body")
			} else if op.RequestBody.ContentType != "application/json" {
				t.Errorf("Create Pet content type = %q, want %q", op.RequestBody.ContentType, "application/json")
			}
			hasHeader := false
			for _, p := range op.Parameters {
				if p.In == "header" && p.Name == "X-Request-Id" {
					hasHeader = true
				}
			}
			if !hasHeader {
				t.Error("Create Pet missing X-Request-Id header param")
			}
		}
	}

	// Verify List Pets has a query parameter.
	for _, op := range svc.Operations {
		if op.ID == "Pets_List_Pets" {
			hasQuery := false
			for _, p := range op.Parameters {
				if p.In == "query" && p.Name == "limit" {
					hasQuery = true
				}
			}
			if !hasQuery {
				t.Error("List Pets missing 'limit' query param")
			}
		}
	}
}

func TestParseToCanonical_BaseURLOverride(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalExport), "test", "https://override.example.com")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}
	if svc.BaseURL != "https://override.example.com" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://override.example.com")
	}
}

func TestParseToCanonical_NoBaseURL(t *testing.T) {
	noEnv := `{
		"_type": "export",
		"__export_format": 4,
		"resources": [
			{"_id": "req_1", "_type": "request", "parentId": null, "name": "Ping", "method": "GET", "url": "/ping", "parameters": [], "headers": [], "body": null}
		]
	}`
	_, err := ParseToCanonical(context.Background(), []byte(noEnv), "test", "")
	if err == nil {
		t.Error("expected error when no base URL is available")
	}
}

func TestParseToCanonical_InvalidJSON(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte("not json"), "test", "https://example.com")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
