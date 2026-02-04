package odata

import (
	"context"
	"testing"
)

const testCSDL = `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx Version="4.0" xmlns:edmx="http://docs.oasis-open.org/odata/ns/edmx">
  <edmx:DataServices>
    <Schema Namespace="MockMovies" xmlns="http://docs.oasis-open.org/odata/ns/edm">
      <EntityType Name="Movie">
        <Key>
          <PropertyRef Name="ID"/>
        </Key>
        <Property Name="ID" Type="Edm.Int64" Nullable="false"/>
        <Property Name="Title" Type="Edm.String" Nullable="false"/>
        <Property Name="Year" Type="Edm.Int32" Nullable="false"/>
        <Property Name="Genre" Type="Edm.String" Nullable="false"/>
        <Property Name="Rating" Type="Edm.Double" Nullable="false"/>
        <Property Name="Director" Type="Edm.String" Nullable="false"/>
      </EntityType>
      <EntityContainer Name="Container">
        <EntitySet Name="Movies" EntityType="MockMovies.Movie"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`

func TestLooksLikeODataMetadata(t *testing.T) {
	if !LooksLikeODataMetadata([]byte(testCSDL)) {
		t.Fatal("expected true for valid CSDL")
	}
	if LooksLikeODataMetadata([]byte(`{"openapi":"3.0.0"}`)) {
		t.Fatal("expected false for OpenAPI JSON")
	}
	if LooksLikeODataMetadata([]byte(`<wsdl:definitions>`)) {
		t.Fatal("expected false for WSDL")
	}
}

func TestParseToCanonical(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(testCSDL), "movies-odata", "http://localhost:9999/odata")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if svc.Name != "movies-odata" {
		t.Fatalf("unexpected service name: %s", svc.Name)
	}
	if svc.BaseURL != "http://localhost:9999/odata" {
		t.Fatalf("unexpected base URL: %s", svc.BaseURL)
	}

	// Should have 5 operations: list, get, create, update, delete
	if len(svc.Operations) != 5 {
		t.Fatalf("expected 5 operations, got %d", len(svc.Operations))
	}

	opMap := map[string]struct{}{}
	for _, op := range svc.Operations {
		opMap[op.ID] = struct{}{}
	}
	for _, id := range []string{"listMovies", "getMovies", "createMovies", "updateMovies", "deleteMovies"} {
		if _, ok := opMap[id]; !ok {
			t.Fatalf("missing operation: %s", id)
		}
	}

	// Check list operation
	for _, op := range svc.Operations {
		if op.ID == "listMovies" {
			if op.Method != "get" {
				t.Fatalf("list method: %s", op.Method)
			}
			if op.Path != "/Movies" {
				t.Fatalf("list path: %s", op.Path)
			}
			if op.QueryParamsObject != "queryOptions" {
				t.Fatalf("list QueryParamsObject: %s", op.QueryParamsObject)
			}
		}
		if op.ID == "getMovies" {
			if op.Method != "get" {
				t.Fatalf("get method: %s", op.Method)
			}
			if op.Path != "/Movies({ID})" {
				t.Fatalf("get path: %s", op.Path)
			}
			if len(op.Parameters) != 1 || op.Parameters[0].Name != "ID" {
				t.Fatalf("get params: %v", op.Parameters)
			}
		}
		if op.ID == "createMovies" {
			if op.Method != "post" {
				t.Fatalf("create method: %s", op.Method)
			}
			if op.RequestBody == nil {
				t.Fatal("create missing request body")
			}
		}
		if op.ID == "updateMovies" {
			if op.Method != "patch" {
				t.Fatalf("update method: %s", op.Method)
			}
		}
		if op.ID == "deleteMovies" {
			if op.Method != "delete" {
				t.Fatalf("delete method: %s", op.Method)
			}
		}
	}
}

func TestParseToCanonical_NoBaseURL(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte(testCSDL), "test", "")
	if err == nil {
		t.Fatal("expected error for missing base URL")
	}
}

func TestEdmTypeMapping(t *testing.T) {
	tests := []struct {
		edm      string
		expected string
	}{
		{"Edm.String", "string"},
		{"Edm.Int32", "integer"},
		{"Edm.Int64", "integer"},
		{"Edm.Double", "number"},
		{"Edm.Boolean", "boolean"},
		{"Edm.DateTimeOffset", "string"},
		{"Edm.Guid", "string"},
	}
	for _, tt := range tests {
		schema := edmTypeToJSONSchema(tt.edm, false)
		if schema["type"] != tt.expected {
			t.Fatalf("%s: expected %s, got %s", tt.edm, tt.expected, schema["type"])
		}
	}
}
