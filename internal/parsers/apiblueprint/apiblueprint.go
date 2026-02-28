package apiblueprint

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"
)

// LooksLikeAPIBlueprint reports whether raw looks like an API Blueprint document.
// API Blueprint files start with "FORMAT: 1A" or contain "# Group" and "## " resource headers.
func LooksLikeAPIBlueprint(raw []byte) bool {
	s := string(raw)
	// Canonical format indicator.
	if strings.Contains(s, "FORMAT: 1A") || strings.Contains(s, "FORMAT:1A") {
		return true
	}
	// Heuristic: contains typical API Blueprint patterns.
	hasGroup := strings.Contains(s, "# Group ")
	hasResource := resourceHeaderRe.MatchString(s)
	hasAction := actionHeaderRe.MatchString(s)
	return (hasGroup && hasResource) || (hasResource && hasAction)
}

// ParseToCanonical parses an API Blueprint document into a canonical Service.
// API Blueprint uses Markdown with structured headers to define resources and actions.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx

	lines := strings.Split(string(raw), "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("apiblueprint: document too short")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	title := apiName

	// Parse metadata and title.
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if baseURL == "" && strings.HasPrefix(trimmed, "HOST:") {
			baseURL = strings.TrimSpace(strings.TrimPrefix(trimmed, "HOST:"))
			baseURL = strings.TrimRight(baseURL, "/")
		}
		// Title is typically the first # heading after metadata.
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "# Group") {
			candidate := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			// Ignore if it looks like a resource (contains []).
			if !strings.Contains(candidate, "[") {
				title = candidate
			}
		}
	}

	if baseURL == "" {
		return nil, fmt.Errorf("apiblueprint: base_url_override is required (or set HOST: in the document)")
	}

	if apiName == "" {
		apiName = title
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	parseActions(service, apiName, lines)

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("apiblueprint: no actions found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

var (
	// Matches resource headers like: ## Resource Name [/path]
	resourceHeaderRe = regexp.MustCompile(`^#{1,3}\s+.+\[(/[^\]]*)\]`)
	// Matches action headers like: ### Action Name [GET /path] or ### Action Name [GET]
	actionHeaderRe = regexp.MustCompile(`^#{1,4}\s+(.+?)\s*\[(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)(?:\s+(/[^\]]*))?\]`)
	// URI parameter pattern.
	uriParamRe = regexp.MustCompile(`\{(\w+)\}`)
)

func parseActions(service *canonical.Service, apiName string, lines []string) {
	currentResourcePath := ""

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		// Check for resource header: ## Name [/path]
		if matches := resourceHeaderRe.FindStringSubmatch(trimmed); len(matches) > 1 {
			currentResourcePath = matches[1]
			continue
		}

		// Check for action header: ### Name [METHOD /path] or ### Name [METHOD]
		if matches := actionHeaderRe.FindStringSubmatch(trimmed); len(matches) > 2 {
			actionName := strings.TrimSpace(matches[1])
			method := strings.ToLower(matches[2])
			actionPath := ""
			if len(matches) > 3 {
				actionPath = strings.TrimSpace(matches[3])
			}

			// If action has its own path, use it; otherwise inherit from resource.
			path := actionPath
			if path == "" {
				path = currentResourcePath
			}
			if path == "" {
				path = "/"
			}

			// Gather description from lines following the action header.
			description := ""
			for j := i + 1; j < len(lines); j++ {
				nextTrimmed := strings.TrimSpace(lines[j])
				if nextTrimmed == "" {
					continue
				}
				// Stop at next header or section marker.
				if strings.HasPrefix(nextTrimmed, "#") || strings.HasPrefix(nextTrimmed, "+") {
					break
				}
				description = nextTrimmed
				break
			}

			op := buildOperation(apiName, actionName, method, path, description)
			if op != nil {
				service.Operations = append(service.Operations, op)
			}
		}
	}
}

func buildOperation(apiName, actionName, method, path, description string) *canonical.Operation {
	operationID := sanitizeName(method + "_" + actionName)
	if operationID == "" {
		operationID = sanitizeName(method + "_" + path)
	}

	toolName := canonical.ToolName(apiName, operationID)

	summary := actionName
	if description != "" && summary == "" {
		summary = description
	}
	if summary == "" {
		summary = strings.ToUpper(method) + " " + path
	}

	var params []canonical.Parameter
	properties := map[string]any{}
	requiredFields := []string{}

	// Extract URI template parameters.
	for _, match := range uriParamRe.FindAllStringSubmatch(path, -1) {
		paramName := match[1]
		params = append(params, canonical.Parameter{
			Name:     paramName,
			In:       "path",
			Required: true,
			Schema:   map[string]any{"type": "string"},
		})
		properties[paramName] = map[string]any{"type": "string"}
		requiredFields = append(requiredFields, paramName)
	}

	// For write methods, add body parameter.
	var reqBody *canonical.RequestBody
	if method == "post" || method == "put" || method == "patch" {
		reqBody = &canonical.RequestBody{
			Required:    true,
			ContentType: "application/json",
			Schema:      map[string]any{"type": "object", "additionalProperties": true},
		}
		properties["body"] = map[string]any{"type": "object", "additionalProperties": true, "description": "Request body"}
		requiredFields = append(requiredFields, "body")
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(requiredFields) > 0 {
		sort.Strings(requiredFields)
		inputSchema["required"] = requiredFields
	}

	return &canonical.Operation{
		ServiceName: apiName,
		ID:          operationID,
		ToolName:    toolName,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Description: description,
		Parameters:  params,
		RequestBody: reqBody,
		InputSchema: inputSchema,
	}
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '-' || r == ' ' || r == '.' || r == '{' || r == '}':
			b.WriteRune('_')
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		}
	}
	result := b.String()
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}
	return strings.Trim(result, "_")
}
