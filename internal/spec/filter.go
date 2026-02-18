package spec

import (
	"path/filepath"
	"strings"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
)

// ApplyOperationFilters filters operations according to filter config.
// This should be called AFTER parsing specs but BEFORE creating the registry.
func ApplyOperationFilters(services []*canonical.Service, apiConfigs []config.APIConfig) []*canonical.Service {
	// Build a map of API name -> filter
	filters := make(map[string]*config.OperationFilterEnhanced)
	for _, api := range apiConfigs {
		if api.Filter != nil {
			filters[api.Name] = api.Filter
		}
	}

	// Apply filters to each service
	filtered := make([]*canonical.Service, 0, len(services))
	for _, svc := range services {
		filter, hasFilter := filters[svc.Name]
		if !hasFilter {
			// No filter = keep all operations (backward compatible)
			filtered = append(filtered, svc)
			continue
		}

		filteredSvc := &canonical.Service{
			Name:       svc.Name,
			BaseURL:    svc.BaseURL,
			Operations: filterOperations(svc.Operations, filter),
		}
		filtered = append(filtered, filteredSvc)
	}

	return filtered
}

// filterOperations applies filter to a list of operations
func filterOperations(ops []*canonical.Operation, filter *config.OperationFilterEnhanced) []*canonical.Operation {
	mode := strings.ToLower(filter.Mode)

	if mode == "type-based" && filter.TypeBased != nil {
		return filterOperationsByType(ops, filter.TypeBased)
	}

	result := make([]*canonical.Operation, 0, len(ops))
	for _, op := range ops {
		matches := operationMatches(op, filter.Operations)

		// Allowlist: keep if matches
		// Blocklist: keep if NOT matches
		keep := (mode == "allowlist" && matches) || (mode == "blocklist" && !matches)

		if keep {
			result = append(result, op)
		}
	}

	return result
}

// filterOperationsByType filters operations based on their GraphQL return type.
// Non-GraphQL operations always pass through.
// For CRUD composites, the Composite.Pattern (base type name) is matched.
// For individual GraphQL operations, ReturnTypeName is matched.
// Exclude takes precedence over include. Empty include = allow all except excluded.
func filterOperationsByType(ops []*canonical.Operation, tb *config.TypeBasedFilter) []*canonical.Operation {
	includeSet := make(map[string]bool, len(tb.IncludeTypes))
	for _, t := range tb.IncludeTypes {
		includeSet[t] = true
	}
	excludeSet := make(map[string]bool, len(tb.ExcludeTypes))
	for _, t := range tb.ExcludeTypes {
		excludeSet[t] = true
	}

	result := make([]*canonical.Operation, 0, len(ops))
	for _, op := range ops {
		if op.GraphQL == nil {
			// Non-GraphQL operations pass through unchanged
			result = append(result, op)
			continue
		}

		typeName := operationTypeName(op)
		if typeName == "" {
			// Can't determine type — keep it
			result = append(result, op)
			continue
		}

		// Exclude takes precedence
		if excludeSet[typeName] {
			continue
		}

		// If include set is non-empty, only keep matching types
		if len(includeSet) > 0 && !includeSet[typeName] {
			continue
		}

		result = append(result, op)
	}

	return result
}

// operationTypeName returns the type name used for type-based filtering.
// For composite (CRUD-grouped) operations, it returns the Composite.Pattern.
// For individual operations, it returns ReturnTypeName.
func operationTypeName(op *canonical.Operation) string {
	if op.GraphQL.Composite != nil && op.GraphQL.Composite.Pattern != "" {
		return op.GraphQL.Composite.Pattern
	}
	return op.GraphQL.ReturnTypeName
}

// operationMatches checks if operation matches ANY of the patterns
func operationMatches(op *canonical.Operation, patterns []config.OperationPattern) bool {
	for _, pattern := range patterns {
		if patternMatches(op, pattern) {
			return true
		}
	}
	return false
}

// patternMatches checks if a single pattern matches the operation
func patternMatches(op *canonical.Operation, pattern config.OperationPattern) bool {
	// If pattern specifies operationId, it must match
	if pattern.OperationID != "" {
		if !globMatch(pattern.OperationID, op.ID) {
			return false
		}
	}

	// If pattern specifies method, it must match
	if pattern.Method != "" {
		methodPattern := strings.ToUpper(pattern.Method)
		opMethod := strings.ToUpper(op.Method)

		if methodPattern != "*" && methodPattern != opMethod {
			return false
		}
	}

	// If pattern specifies path, it must match
	if pattern.Path != "" {
		if !globMatch(pattern.Path, op.Path) {
			return false
		}
	}

	// All specified fields matched
	return true
}

// globMatch performs glob pattern matching with * and ?
// * matches any sequence of characters
// ** matches any sequence including path separators
// ? matches any single character
func globMatch(pattern, str string) bool {
	// Handle ** in paths (match across path separators)
	// Replace ** with a special marker that we'll handle
	if strings.Contains(pattern, "**") {
		// For **, we want to match everything, so just check if str contains the parts around **
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			// Pattern like "/admin/**" or "**/admin"
			if parts[0] != "" && !strings.HasPrefix(str, strings.TrimSuffix(parts[0], "/")) {
				return false
			}
			if parts[1] != "" && !strings.HasSuffix(str, strings.TrimPrefix(parts[1], "/")) {
				return false
			}
			return true
		}
	}

	// Use filepath.Match for standard glob matching (*, ?)
	matched, err := filepath.Match(pattern, str)
	if err != nil {
		// If pattern is invalid, be conservative and don't match
		return false
	}

	return matched
}
