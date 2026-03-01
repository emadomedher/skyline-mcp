package raml

import (
	"context"
	"testing"
)

const minimalRAML = `#%RAML 1.0
title: Pet API
baseUri: https://api.example.com

/pets:
  get:
    description: List all pets
  post:
    description: Create a pet
  /{petId}:
    get:
      description: Get a pet by ID
`

func TestLooksLikeRAML(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"RAML 1.0 header", "#%RAML 1.0\ntitle: My API", true},
		{"RAML 0.8 header", "#%RAML 0.8\ntitle: Old API", true},
		{"openapi json", `{"openapi":"3.0.0"}`, false},
		{"plain text", "hello world", false},
		{"empty", "", false},
		{"yaml without RAML header", "title: My API\nbaseUri: http://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeRAML([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeRAML() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalRAML), "petapi", "")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "petapi" {
		t.Errorf("Name = %q, want %q", svc.Name, "petapi")
	}
	if svc.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://api.example.com")
	}

	if len(svc.Operations) != 3 {
		t.Fatalf("len(Operations) = %d, want 3", len(svc.Operations))
	}

	ops := map[string]struct{ Method, Path string }{}
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, Path string }{op.Method, op.Path}
	}

	// operationID = sanitizeName(method + "_" + path)
	// sanitizeName("get_/pets") => "get_pets"
	if op, ok := ops["get_pets"]; !ok {
		t.Errorf("missing get_pets; have %v", ops)
	} else {
		if op.Method != "get" {
			t.Errorf("get_pets method = %q, want %q", op.Method, "get")
		}
		if op.Path != "/pets" {
			t.Errorf("get_pets path = %q, want %q", op.Path, "/pets")
		}
	}

	// sanitizeName("post_/pets") => "post_pets"
	if op, ok := ops["post_pets"]; !ok {
		t.Errorf("missing post_pets; have %v", ops)
	} else {
		if op.Method != "post" {
			t.Errorf("post_pets method = %q, want %q", op.Method, "post")
		}
	}

	// sanitizeName("get_/pets/{petId}") => "get_pets_petId"
	if op, ok := ops["get_pets_petId"]; !ok {
		t.Errorf("missing get_pets_petId; have %v", ops)
	} else {
		if op.Method != "get" {
			t.Errorf("get_pets_petId method = %q, want %q", op.Method, "get")
		}
		if op.Path != "/pets/{petId}" {
			t.Errorf("get_pets_petId path = %q, want %q", op.Path, "/pets/{petId}")
		}
	}

	// Verify the nested resource has a path parameter.
	for _, op := range svc.Operations {
		if op.ID == "get_pets_petId" {
			foundParam := false
			for _, p := range op.Parameters {
				if p.Name == "petId" && p.In == "path" && p.Required {
					foundParam = true
				}
			}
			if !foundParam {
				t.Error("get_pets_petId missing required 'petId' path parameter")
			}
		}
	}

	// Verify post_pets has a request body.
	for _, op := range svc.Operations {
		if op.ID == "post_pets" {
			if op.RequestBody == nil {
				t.Error("post_pets should have a request body")
			}
		}
	}
}

func TestParseToCanonical_BaseURLOverride(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalRAML), "test", "https://override.example.com")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}
	if svc.BaseURL != "https://override.example.com" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://override.example.com")
	}
}

func TestParseToCanonical_NoBaseURL(t *testing.T) {
	noBase := `#%RAML 1.0
title: No Base

/items:
  get:
    description: List items
`
	_, err := ParseToCanonical(context.Background(), []byte(noBase), "test", "")
	if err == nil {
		t.Error("expected error when no base URL is available")
	}
}

func TestParseToCanonical_TooShort(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte("#%RAML 1.0"), "test", "https://example.com")
	if err == nil {
		t.Error("expected error for too-short document")
	}
}
