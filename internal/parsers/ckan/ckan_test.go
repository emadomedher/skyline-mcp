package ckan

import (
	"context"
	"testing"
)

func TestLooksLikeCKAN(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"empty input (nil-like)", "", true},
		{"whitespace only", "   \n  ", true},
		{"valid CKAN response", `{"success":true,"help":"https://data.gov/api/3/action/package_list","result":["dataset1"]}`, true},
		{"json without CKAN markers", `{"success":true,"help":"https://example.com/other","result":"ok"}`, false},
		{"openapi doc", `{"openapi":"3.0.0"}`, false},
		{"not json", "hello world", false},
		{"json array", `[1,2,3]`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeCKAN([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeCKAN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), nil, "opendata", "https://data.gov")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "opendata" {
		t.Errorf("Name = %q, want %q", svc.Name, "opendata")
	}
	if svc.BaseURL != "https://data.gov" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://data.gov")
	}

	if len(svc.Operations) != 7 {
		t.Fatalf("len(Operations) = %d, want 7", len(svc.Operations))
	}

	// Build lookup by operation ID.
	ops := map[string]struct{ Method, Path string }{}
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, Path string }{op.Method, op.Path}
	}

	// Spot-check searchDatasets.
	if op, ok := ops["searchDatasets"]; !ok {
		t.Errorf("missing searchDatasets; have %v", ops)
	} else {
		if op.Method != "post" {
			t.Errorf("searchDatasets method = %q, want %q", op.Method, "post")
		}
		if op.Path != "/api/3/action/package_search" {
			t.Errorf("searchDatasets path = %q, want %q", op.Path, "/api/3/action/package_search")
		}
	}

	// Verify all 7 expected IDs.
	expectedIDs := []string{
		"searchDatasets", "listDatasets", "getDataset",
		"getResource", "queryDatastore", "listOrganizations", "listTags",
	}
	for _, id := range expectedIDs {
		if _, ok := ops[id]; !ok {
			t.Errorf("missing operation %q", id)
		}
	}
}

func TestParseToCanonical_MissingBaseURL(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), nil, "test", "")
	if err == nil {
		t.Error("expected error when base URL is empty")
	}
}

func TestParseToCanonical_TrailingSlash(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), nil, "test", "https://data.gov/")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}
	if svc.BaseURL != "https://data.gov" {
		t.Errorf("BaseURL = %q, want trailing slash stripped to %q", svc.BaseURL, "https://data.gov")
	}
}
