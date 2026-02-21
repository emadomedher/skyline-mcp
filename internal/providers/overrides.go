package providers

import (
	"log"
	"path/filepath"
	"strings"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
)

// ProviderOverride defines built-in operation blocklists for a known API provider.
// Each override specifies detection heuristics (name/URL patterns) and a set of
// operation patterns that should be blocked because they are known-broken.
type ProviderOverride struct {
	// Provider is a human-readable identifier (e.g., "jira-cloud").
	Provider string

	// Reason is a short explanation logged when operations are filtered.
	Reason string

	// MatchName contains lowercase substrings checked against the API name.
	// If ANY substring matches (case-insensitive), the override applies.
	MatchName []string

	// MatchSpecURL contains lowercase substrings checked against the spec URL.
	// If ANY substring matches (case-insensitive), the override applies.
	MatchSpecURL []string

	// BlockPatterns are the operation patterns to filter out (blocklist semantics).
	// Uses the same OperationPattern type as user-configured filters.
	BlockPatterns []config.OperationPattern
}

// registry holds all built-in provider overrides.
var registry []ProviderOverride

// Register adds a provider override to the built-in registry.
// Called from init() functions in provider-specific files.
func Register(o ProviderOverride) {
	registry = append(registry, o)
}

// AllOverrides returns a copy of the current registry.
func AllOverrides() []ProviderOverride {
	out := make([]ProviderOverride, len(registry))
	copy(out, registry)
	return out
}

// DetectOverrides returns all ProviderOverride entries that match the given
// API name and/or spec URL. Multiple providers can match the same API.
func DetectOverrides(apiName, specURL string) []ProviderOverride {
	nameL := strings.ToLower(apiName)
	urlL := strings.ToLower(specURL)

	var matched []ProviderOverride
	for _, o := range registry {
		if matchesProvider(o, nameL, urlL) {
			matched = append(matched, o)
		}
	}
	return matched
}

func matchesProvider(o ProviderOverride, nameL, urlL string) bool {
	for _, sub := range o.MatchName {
		if strings.Contains(nameL, sub) {
			return true
		}
	}
	for _, sub := range o.MatchSpecURL {
		if urlL != "" && strings.Contains(urlL, sub) {
			return true
		}
	}
	return false
}

// ApplyProviderOverrides applies built-in provider-specific blocklists to services.
// Operations matching known-bad patterns are removed with a log entry explaining why.
// APIs with DisableProviderOverrides=true are skipped entirely.
func ApplyProviderOverrides(services []*canonical.Service, apiConfigs []config.APIConfig, logger *log.Logger) []*canonical.Service {
	// Build lookup: API name -> APIConfig
	configByName := make(map[string]config.APIConfig, len(apiConfigs))
	for _, api := range apiConfigs {
		configByName[api.Name] = api
	}

	result := make([]*canonical.Service, 0, len(services))
	for _, svc := range services {
		apiCfg, ok := configByName[svc.Name]
		if !ok {
			result = append(result, svc)
			continue
		}

		if apiCfg.DisableProviderOverrides {
			logger.Printf("provider overrides disabled for %s (user opt-out)", svc.Name)
			result = append(result, svc)
			continue
		}

		overrides := DetectOverrides(apiCfg.Name, apiCfg.SpecURL)
		if len(overrides) == 0 {
			result = append(result, svc)
			continue
		}

		// Collect all block patterns from all matching overrides
		var allPatterns []config.OperationPattern
		for _, o := range overrides {
			logger.Printf("provider override %q matched for %s: %s", o.Provider, svc.Name, o.Reason)
			allPatterns = append(allPatterns, o.BlockPatterns...)
		}

		filtered := applyBlocklist(svc.Operations, allPatterns, svc.Name, logger)

		removed := len(svc.Operations) - len(filtered)
		if removed > 0 {
			logger.Printf("provider overrides for %s: removed %d known-broken operations (%d remaining)",
				svc.Name, removed, len(filtered))
		}

		result = append(result, &canonical.Service{
			Name:       svc.Name,
			BaseURL:    svc.BaseURL,
			Operations: filtered,
		})
	}

	return result
}

// applyBlocklist removes operations matching any of the block patterns.
func applyBlocklist(ops []*canonical.Operation, patterns []config.OperationPattern, serviceName string, logger *log.Logger) []*canonical.Operation {
	result := make([]*canonical.Operation, 0, len(ops))
	for _, op := range ops {
		if operationMatchesAny(op, patterns) {
			logger.Printf("  blocked %s %s (operation: %s) — provider override", op.Method, op.Path, op.ID)
			continue
		}
		result = append(result, op)
	}
	return result
}

// operationMatchesAny checks if an operation matches ANY of the given patterns.
// Duplicated from internal/spec/filter.go to avoid circular imports.
func operationMatchesAny(op *canonical.Operation, patterns []config.OperationPattern) bool {
	for _, pattern := range patterns {
		if patternMatches(op, pattern) {
			return true
		}
	}
	return false
}

// patternMatches checks if a single pattern matches the operation.
// All specified fields must match (AND logic); unspecified fields are ignored.
func patternMatches(op *canonical.Operation, pattern config.OperationPattern) bool {
	if pattern.OperationID != "" {
		if !globMatch(pattern.OperationID, op.ID) {
			return false
		}
	}
	if pattern.Method != "" {
		methodPattern := strings.ToUpper(pattern.Method)
		opMethod := strings.ToUpper(op.Method)
		if methodPattern != "*" && methodPattern != opMethod {
			return false
		}
	}
	if pattern.Path != "" {
		if !globMatch(pattern.Path, op.Path) {
			return false
		}
	}
	return true
}

// globMatch performs glob pattern matching with *, **, and ?.
func globMatch(pattern, str string) bool {
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			if parts[0] != "" && !strings.HasPrefix(str, strings.TrimSuffix(parts[0], "/")) {
				return false
			}
			if parts[1] != "" && !strings.HasSuffix(str, strings.TrimPrefix(parts[1], "/")) {
				return false
			}
			return true
		}
	}
	matched, err := filepath.Match(pattern, str)
	if err != nil {
		return false
	}
	return matched
}
