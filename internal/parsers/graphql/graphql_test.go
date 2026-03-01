package graphql

import (
	"context"
	"testing"
)

const minimalSDL = `type Query {
  hello: String
  user(id: ID!): User
}

type User {
  id: ID
  name: String
  email: String
}
`

const introspectionJSON = `{
  "data": {
    "__schema": {
      "queryType": {"name": "Query"},
      "mutationType": null,
      "types": [
        {
          "kind": "OBJECT",
          "name": "Query",
          "fields": [
            {
              "name": "hello",
              "description": "A greeting",
              "args": [],
              "type": {"kind": "SCALAR", "name": "String", "ofType": null}
            }
          ]
        },
        {"kind": "SCALAR", "name": "String", "fields": null},
        {"kind": "SCALAR", "name": "Boolean", "fields": null}
      ]
    }
  }
}`

func TestLooksLikeGraphQLSDL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"type Query", "type Query { hello: String }", true},
		{"type Mutation", "type Mutation { createUser(name: String!): User }", true},
		{"extend query", "extend type Query { newField: String }", true},
		{"schema block", "schema { query: Query }", true},
		{"json doc", `{"openapi":"3.0.0"}`, false},
		{"plain text", "hello world", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeGraphQLSDL([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeGraphQLSDL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLooksLikeGraphQLIntrospection(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"valid introspection", introspectionJSON, true},
		{"empty data", `{"data":{"__schema":{"types":null}}}`, false},
		{"not json", "type Query { hello: String }", false},
		{"empty", "", false},
		{"random json", `{"foo":"bar"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LooksLikeGraphQLIntrospection([]byte(tt.raw))
			if got != tt.want {
				t.Errorf("LooksLikeGraphQLIntrospection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseToCanonical_SDL(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(minimalSDL), "myapi", "https://api.example.com/graphql")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "myapi" {
		t.Errorf("Name = %q, want %q", svc.Name, "myapi")
	}
	if svc.BaseURL != "https://api.example.com/graphql" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://api.example.com/graphql")
	}

	// Query has 2 user-defined fields + 2 introspection fields (__schema, __type)
	if len(svc.Operations) < 2 {
		t.Fatalf("len(Operations) = %d, want at least 2", len(svc.Operations))
	}

	ops := map[string]struct{ Method, ID string }{}
	for _, op := range svc.Operations {
		ops[op.ID] = struct{ Method, ID string }{op.Method, op.ID}
	}

	// operationID = "query_hello"
	if _, ok := ops["query_hello"]; !ok {
		t.Errorf("missing query_hello; have %v", ops)
	}

	// operationID = "query_user" — has required arg id: ID!
	if _, ok := ops["query_user"]; !ok {
		t.Errorf("missing query_user; have %v", ops)
	}

	// Spot-check that query_user has the id parameter as required.
	for _, op := range svc.Operations {
		if op.ID == "query_user" {
			if op.Method != "post" {
				t.Errorf("query_user method = %q, want %q", op.Method, "post")
			}
			foundID := false
			for _, p := range op.Parameters {
				if p.Name == "id" && p.Required {
					foundID = true
				}
			}
			if !foundID {
				t.Error("query_user missing required 'id' parameter")
			}
		}
	}
}

func TestParseToCanonical_Introspection(t *testing.T) {
	svc, err := ParseToCanonical(context.Background(), []byte(introspectionJSON), "introapi", "https://api.example.com/graphql")
	if err != nil {
		t.Fatalf("ParseToCanonical failed: %v", err)
	}

	if svc.Name != "introapi" {
		t.Errorf("Name = %q, want %q", svc.Name, "introapi")
	}
	if svc.BaseURL != "https://api.example.com/graphql" {
		t.Errorf("BaseURL = %q, want %q", svc.BaseURL, "https://api.example.com/graphql")
	}

	// Query type has 1 field: hello
	if len(svc.Operations) != 1 {
		t.Fatalf("len(Operations) = %d, want 1", len(svc.Operations))
	}

	op := svc.Operations[0]
	if op.ID != "query_hello" {
		t.Errorf("op.ID = %q, want %q", op.ID, "query_hello")
	}
	if op.Method != "post" {
		t.Errorf("op.Method = %q, want %q", op.Method, "post")
	}
	if op.Summary != "A greeting" {
		t.Errorf("op.Summary = %q, want %q", op.Summary, "A greeting")
	}
}

func TestParseToCanonical_SDL_NoBaseURL(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte(minimalSDL), "test", "")
	if err == nil {
		t.Error("expected error when base URL is empty")
	}
}

func TestParseToCanonical_Introspection_NoBaseURL(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte(introspectionJSON), "test", "")
	if err == nil {
		t.Error("expected error when base URL is empty")
	}
}

func TestParseToCanonical_UnsupportedPayload(t *testing.T) {
	_, err := ParseToCanonical(context.Background(), []byte("not graphql at all"), "test", "https://example.com")
	if err == nil {
		t.Error("expected error for unsupported payload")
	}
}
