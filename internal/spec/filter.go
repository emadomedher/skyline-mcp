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
	filters := make(map[string]*config.OperationFilter)
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
func filterOperations(ops []*canonical.Operation, filter *config.OperationFilter) []*canonical.Operation {
	mode := strings.ToLower(filter.Mode)

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
