package apiblueprint

import (
	"context"
	"testing"
)

const minimalBlueprint = `FORMAT: 1A
HOST: https://api.example.com

# My API

## Pets [/pets]

### List Pets [GET]

Returns all pets

### Create Pet [POST]

Creates a pet
`

func TestLooksLikeAPIBlueprint(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"FORMAT 1A header", "FORMAT: 1A\nHOST: https://example.com\n# API", true},
		{"FORMAT 1A no space", "FORMAT:1A\nHOST: https://example.com", true},
		{"resource and action at start", "## Users [/users]\n### List Users [GET]", false},
		{"group and resource", "# Group Pets\n## Pets [/pets]", false},
		{"FORMAT 1A in body", "Some header\nFORMAT: 1A\nMore stuff", true},
		{"openapi doc", `{"openapi":"3.0.0"}`, false},
		{"plain text", "Hello world", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeAPIBlueprint([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeAPIBlueprint() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalBlueprint), "petapi", "")
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

	// Operations are sorted by ToolName. Build lookup by ID.
	ops := map[string]struct{ Method, Path string }{}
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, Path string }{op.Method, op.Path}
	}

	// sanitizeName("get_List Pets") => "get_List_Pets"
	if op, ok := ops["get_List_Pets"]; !ok {
		t.Errorf("missing get_List_Pets operation; have %v", ops)
	} else {
		if op.Method != "get" {
			t.Errorf("List Pets method = %q, want %q", op.Method, "get")
		}
		if op.Path != "/pets" {
			t.Errorf("List Pets path = %q, want %q", op.Path, "/pets")
		}
	}

	if op, ok := ops["post_Create_Pet"]; !ok {
		t.Errorf("missing post_Create_Pet operation; have %v", ops)
	} else {
		if op.Method != "post" {
			t.Errorf("Create Pet method = %q, want %q", op.Method, "post")
		}
		if op.Path != "/pets" {
			t.Errorf("Create Pet path = %q, want %q", op.Path, "/pets")
		}
	}

	// Verify Create Pet has a request body.
	for _, op := range svc.Operations {
		if op.ID == "post_Create_Pet" {
			if op.RequestBody == nil {
				t.Error("Create Pet should have a request body")
			}
		}
	}
}

func TestParseToCanonical_BaseURLOverride(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalBlueprint), "test", "https://override.example.com")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}
	if svc.BaseURL != "https://override.example.com" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://override.example.com")
	}
}

func TestParseToCanonical_NoBaseURL(t *testing.T) {
	noHost := `FORMAT: 1A

# My API

## Pets [/pets]

### List Pets [GET]

Returns all pets
`
	_, err := ParseToCanonical(context.Background(), []byte(noHost), "test", "")
	if err == nil {
		t.Error("expected error when no base URL is provided")
	}
}

func TestParseToCanonical_EmptyInput(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte(""), "test", "https://example.com")
	if err == nil {
		t.Error("expected error for empty input")
	}
}
