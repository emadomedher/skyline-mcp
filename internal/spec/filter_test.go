package spec

import (
	"testing"

	"mcp-api-bridge/internal/canonical"
	"mcp-api-bridge/internal/config"
)

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern string
		str     string
		want    bool
	}{
		// operationId patterns
		{"getPet*", "getPetById", true},
		{"getPet*", "getPets", true},
		{"getPet*", "createPet", false},
		{"*User", "createUser", true},
		{"*User", "getUser", true},
		{"*User", "getUserById", false},
		{"*", "anything", true},

		// Path patterns with **
		{"/users/**", "/users/123", true},
		{"/users/**", "/users/123/orders", true},
		{"/users/**", "/users/123/orders/456", true},
		{"/admin/**", "/admin/users", true},
		{"/admin/**", "/users/admin", false},
		{"**/admin", "/api/admin", true},
		{"**/admin", "/api/v1/admin", true},

		// Simple path patterns
		{"/users/*", "/users/123", true},
		{"/users/*", "/users/123/orders", false},
		{"/pets/{id}", "/pets/{id}", true},

		// Exact matches
		{"getPetById", "getPetById", true},
		{"getPetById", "getPets", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.str, func(t *testing.T) {
			got := globMatch(tt.pattern, tt.str)
			if got != tt.want {
				t.Errorf("globMatch(%q, %q) = %v, want %v", tt.pattern, tt.str, got, tt.want)
			}
		})
	}
}

func TestPatternMatches(t *testing.T) {
	op := &canonical.Operation{
		ID:     "getPetById",
		Method: "GET",
		Path:   "/pets/{petId}",
	}

	tests := []struct {
		name    string
		pattern config.OperationPattern
		want    bool
	}{
		{
			name:    "matches operation_id",
			pattern: config.OperationPattern{OperationID: "getPet*"},
			want:    true,
		},
		{
			name:    "matches method",
			pattern: config.OperationPattern{Method: "GET"},
			want:    true,
		},
		{
			name:    "matches path",
			pattern: config.OperationPattern{Path: "/pets/*"},
			want:    true,
		},
		{
			name:    "matches combined (AND logic)",
			pattern: config.OperationPattern{Method: "GET", Path: "/pets/*"},
			want:    true,
		},
		{
			name:    "no match - wrong operation_id",
			pattern: config.OperationPattern{OperationID: "createPet"},
			want:    false,
		},
		{
			name:    "no match - wrong method",
			pattern: config.OperationPattern{Method: "POST"},
			want:    false,
		},
		{
			name:    "no match - combined fails",
			pattern: config.OperationPattern{Method: "POST", Path: "/pets/*"},
			want:    false,
		},
		{
			name:    "wildcard method matches",
			pattern: config.OperationPattern{Method: "*"},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := patternMatches(op, tt.pattern)
			if got != tt.want {
				t.Errorf("patternMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperationMatches(t *testing.T) {
	op := &canonical.Operation{
		ID:     "getPetById",
		Method: "GET",
		Path:   "/pets/{petId}",
	}

	tests := []struct {
		name     string
		patterns []config.OperationPattern
		want     bool
	}{
		{
			name: "matches one pattern (OR logic)",
			patterns: []config.OperationPattern{
				{OperationID: "createPet"},
				{OperationID: "getPet*"},
			},
			want: true,
		},
		{
			name: "matches none",
			patterns: []config.OperationPattern{
				{OperationID: "createPet"},
				{OperationID: "deletePet"},
			},
			want: false,
		},
		{
			name: "empty patterns",
			patterns: []config.OperationPattern{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := operationMatches(op, tt.patterns)
			if got != tt.want {
				t.Errorf("operationMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterOperations_Allowlist(t *testing.T) {
	ops := []*canonical.Operation{
		{ID: "getPetById", Method: "GET", Path: "/pets/{petId}"},
		{ID: "createPet", Method: "POST", Path: "/pets"},
		{ID: "deletePet", Method: "DELETE", Path: "/pets/{petId}"},
		{ID: "updatePet", Method: "PUT", Path: "/pets/{petId}"},
	}

	filter := &config.OperationFilter{
		Mode: "allowlist",
		Operations: []config.OperationPattern{
			{OperationID: "get*"},
			{OperationID: "create*"},
		},
	}

	result := filterOperations(ops, filter)

	if len(result) != 2 {
		t.Errorf("expected 2 operations, got %d", len(result))
	}

	ids := make(map[string]bool)
	for _, op := range result {
		ids[op.ID] = true
	}

	if !ids["getPetById"] || !ids["createPet"] {
		t.Errorf("expected getPetById and createPet, got %v", ids)
	}
	if ids["deletePet"] || ids["updatePet"] {
		t.Errorf("unexpected operations in result: %v", ids)
	}
}

func TestFilterOperations_Blocklist(t *testing.T) {
	ops := []*canonical.Operation{
		{ID: "getPetById", Method: "GET", Path: "/pets/{petId}"},
		{ID: "createPet", Method: "POST", Path: "/pets"},
		{ID: "deletePet", Method: "DELETE", Path: "/pets/{petId}"},
		{ID: "updatePet", Method: "PUT", Path: "/pets/{petId}"},
	}

	filter := &config.OperationFilter{
		Mode: "blocklist",
		Operations: []config.OperationPattern{
			{Method: "DELETE"},
			{OperationID: "update*"},
		},
	}

	result := filterOperations(ops, filter)

	if len(result) != 2 {
		t.Errorf("expected 2 operations, got %d", len(result))
	}

	ids := make(map[string]bool)
	for _, op := range result {
		ids[op.ID] = true
	}

	if !ids["getPetById"] || !ids["createPet"] {
		t.Errorf("expected getPetById and createPet, got %v", ids)
	}
	if ids["deletePet"] || ids["updatePet"] {
		t.Errorf("unexpected operations in result: %v", ids)
	}
}

func TestFilterOperations_MethodAndPath(t *testing.T) {
	ops := []*canonical.Operation{
		{ID: "getUser", Method: "GET", Path: "/users/{id}"},
		{ID: "deleteUser", Method: "DELETE", Path: "/users/{id}"},
		{ID: "getAdmin", Method: "GET", Path: "/admin/users"},
		{ID: "deleteAdmin", Method: "DELETE", Path: "/admin/users"},
	}

	filter := &config.OperationFilter{
		Mode: "blocklist",
		Operations: []config.OperationPattern{
			{Method: "DELETE"},               // Block all DELETE
			{Path: "/admin/**"},              // Block all admin paths
		},
	}

	result := filterOperations(ops, filter)

	// Should only keep getUser (GET /users/{id})
	if len(result) != 1 {
		t.Errorf("expected 1 operation, got %d", len(result))
	}

	if len(result) > 0 && result[0].ID != "getUser" {
		t.Errorf("expected getUser, got %s", result[0].ID)
	}
}

func TestApplyOperationFilters(t *testing.T) {
	services := []*canonical.Service{
		{
			Name: "api1",
			Operations: []*canonical.Operation{
				{ID: "op1", Method: "GET", Path: "/path1"},
				{ID: "op2", Method: "POST", Path: "/path2"},
				{ID: "op3", Method: "DELETE", Path: "/path3"},
			},
		},
		{
			Name: "api2",
			Operations: []*canonical.Operation{
				{ID: "op4", Method: "GET", Path: "/path4"},
				{ID: "op5", Method: "POST", Path: "/path5"},
			},
		},
	}

	configs := []config.APIConfig{
		{
			Name: "api1",
			Filter: &config.OperationFilter{
				Mode: "allowlist",
				Operations: []config.OperationPattern{
					{Method: "GET"},
					{Method: "POST"},
				},
			},
		},
		// api2 has no filter
		{
			Name: "api2",
		},
	}

	result := ApplyOperationFilters(services, configs)

	// Should have 2 services
	if len(result) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result))
	}

	// api1 should have 2 operations (GET, POST only)
	if len(result[0].Operations) != 2 {
		t.Errorf("api1: expected 2 operations, got %d", len(result[0].Operations))
	}

	// api2 should have all 2 operations (no filter)
	if len(result[1].Operations) != 2 {
		t.Errorf("api2: expected 2 operations, got %d", len(result[1].Operations))
	}

	// Verify api1 has correct operations
	api1Methods := make(map[string]bool)
	for _, op := range result[0].Operations {
		api1Methods[op.Method] = true
	}
	if !api1Methods["GET"] || !api1Methods["POST"] {
		t.Errorf("api1 should have GET and POST, got %v", api1Methods)
	}
	if api1Methods["DELETE"] {
		t.Errorf("api1 should not have DELETE")
	}
}

func TestApplyOperationFilters_NoFilters(t *testing.T) {
	services := []*canonical.Service{
		{
			Name: "api1",
			Operations: []*canonical.Operation{
				{ID: "op1", Method: "GET", Path: "/path1"},
				{ID: "op2", Method: "POST", Path: "/path2"},
			},
		},
	}

	configs := []config.APIConfig{
		{Name: "api1"}, // No filter
	}

	result := ApplyOperationFilters(services, configs)

	// Should have 1 service with all operations (backward compatible)
	if len(result) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result))
	}

	if len(result[0].Operations) != 2 {
		t.Errorf("expected 2 operations, got %d", len(result[0].Operations))
	}
}
