package raml

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"
)

// LooksLikeRAML reports whether raw looks like a RAML document.
func LooksLikeRAML(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "#%RAML")
}

// ParseToCanonical parses a RAML 0.8/1.0 document into a canonical Service.
// RAML is YAML-based; we parse it line-by-line for the structural elements
// needed to produce canonical operations (resources, methods, parameters).
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx

	lines := strings.Split(string(raw), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("raml: document too short")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	title := apiName

	// Parse header fields.
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if baseURL == "" && strings.HasPrefix(trimmed, "baseUri:") {
			baseURL = strings.TrimSpace(strings.TrimPrefix(trimmed, "baseUri:"))
			baseURL = strings.Trim(baseURL, "\"'")
			baseURL = strings.TrimRight(baseURL, "/")
		}
		if strings.HasPrefix(trimmed, "title:") {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "title:"))
			title = strings.Trim(title, "\"'")
		}
	}

	if baseURL == "" {
		return nil, fmt.Errorf("raml: base_url_override is required (or set baseUri in the RAML document)")
	}

	// Remove baseUri version template.
	baseURL = versionRe.ReplaceAllString(baseURL, "")
	baseURL = strings.TrimRight(baseURL, "/")

	if apiName == "" {
		apiName = title
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	// Parse resources (lines starting with /).
	parseResources(service, apiName, lines)

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("raml: no resources/methods found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

var (
	httpMethods = map[string]bool{
		"get": true, "post": true, "put": true, "patch": true,
		"delete": true, "head": true, "options": true,
	}
	versionRe  = regexp.MustCompile(`/?\{version\}`)
	uriParamRe = regexp.MustCompile(`\{(\w+)\}`)
)

type resourceEntry struct {
	path   string
	indent int
}

func parseResources(service *canonical.Service, apiName string, lines []string) {
	// Stack of resource paths by indentation level.
	var stack []resourceEntry

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		indent := countIndent(line)
		trimmed := strings.TrimSpace(line)

		// Resource line: starts with /
		if strings.HasPrefix(trimmed, "/") {
			resourcePath := strings.TrimRight(strings.Split(trimmed, ":")[0], " ")

			// Pop stack to find parent.
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}

			fullPath := resourcePath
			if len(stack) > 0 {
				fullPath = stack[len(stack)-1].path + resourcePath
			}

			stack = append(stack, resourceEntry{path: fullPath, indent: indent})
			continue
		}

		// Method line: get:, post:, etc.
		methodName := strings.TrimSuffix(trimmed, ":")
		if !httpMethods[methodName] {
			continue
		}
		if len(stack) == 0 {
			continue
		}

		currentPath := stack[len(stack)-1].path

		// Gather description from subsequent lines.
		description := ""
		for j := i + 1; j < len(lines); j++ {
			nextTrimmed := strings.TrimSpace(lines[j])
			if strings.HasPrefix(nextTrimmed, "description:") {
				description = strings.TrimSpace(strings.TrimPrefix(nextTrimmed, "description:"))
				description = strings.Trim(description, "\"'")
				break
			}
			nextIndent := countIndent(lines[j])
			if nextIndent <= indent && nextTrimmed != "" {
				break
			}
		}

		op := buildOperation(apiName, methodName, currentPath, description)
		if op != nil {
			service.Operations = append(service.Operations, op)
		}
	}
}

func buildOperation(apiName, method, path, description string) *canonical.Operation {
	operationID := sanitizeName(method + "_" + path)
	toolName := canonical.ToolName(apiName, operationID)

	summary := strings.ToUpper(method) + " " + path
	if description != "" {
		summary = description
	}

	var params []canonical.Parameter
	properties := map[string]any{}
	requiredFields := []string{}

	// Extract URI parameters.
	for _, match := range uriParamRe.FindAllStringSubmatch(path, -1) {
		paramName := match[1]
		if paramName == "version" {
			continue
		}
		params = append(params, canonical.Parameter{
			Name:     paramName,
			In:       "path",
			Required: true,
			Schema:   map[string]any{"type": "string"},
		})
		properties[paramName] = map[string]any{"type": "string"}
		requiredFields = append(requiredFields, paramName)
	}

	// For POST/PUT/PATCH, add body parameter.
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
		Parameters:  params,
		RequestBody: reqBody,
		InputSchema: inputSchema,
	}
}

func countIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 2
		} else {
			break
		}
	}
	return count
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
	// Collapse consecutive underscores and trim edges.
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}
	return strings.Trim(result, "_")
}

// marshalForDebug is used for testing — convert a Service to JSON.
func marshalForDebug(svc *canonical.Service) string {
	b, _ := json.MarshalIndent(svc, "", "  ")
	return string(b)
}
