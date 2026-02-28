package insomnia

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"
)

// LooksLikeInsomniaCollection reports whether raw looks like an Insomnia export v4.
func LooksLikeInsomniaCollection(raw []byte) bool {
	var doc struct {
		Type      string `json:"_type"`
		ExportFmt int    `json:"__export_format"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	return doc.Type == "export" && doc.ExportFmt >= 4
}

// ParseToCanonical parses an Insomnia v4 export JSON into a canonical Service.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx

	var doc Export
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("insomnia: decode failed: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		// Try to extract from environment resources.
		for _, res := range doc.Resources {
			if res.Type == "environment" && res.Data != nil {
				if bu, ok := res.Data["base_url"]; ok {
					if s, ok := bu.(string); ok && s != "" {
						baseURL = strings.TrimRight(s, "/")
						break
					}
				}
			}
		}
	}
	if baseURL == "" {
		return nil, fmt.Errorf("insomnia: base_url_override is required (or set base_url in the environment)")
	}

	// Build folder name lookup.
	folderNames := map[string]string{}
	for _, res := range doc.Resources {
		if res.Type == "request_group" {
			folderNames[res.ID] = res.Name
		}
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	for _, res := range doc.Resources {
		if res.Type != "request" {
			continue
		}

		prefix := buildFolderPrefix(res.ParentID, folderNames, doc.Resources)
		op := buildOperation(apiName, res, prefix, baseURL)
		if op != nil {
			service.Operations = append(service.Operations, op)
		}
	}

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("insomnia: no request items found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

var insomniaVarRe = regexp.MustCompile(`\{\{\s*(\w+)\s*\}\}`)

func buildOperation(apiName string, res Resource, prefix, baseURL string) *canonical.Operation {
	method := strings.ToLower(res.Method)
	if method == "" {
		method = "get"
	}

	rawURL := res.URL
	// Replace Insomnia template variables {{ var }} with {var}.
	rawURL = insomniaVarRe.ReplaceAllString(rawURL, "{$1}")

	// Extract path by stripping base URL.
	path := rawURL
	if strings.HasPrefix(path, baseURL) {
		path = strings.TrimPrefix(path, baseURL)
	} else if strings.HasPrefix(path, "{") {
		// URL starts with a variable — keep as path.
		path = "/" + path
	}
	// Strip scheme+host if still absolute.
	if idx := strings.Index(path, "://"); idx != -1 {
		rest := path[idx+3:]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			path = rest[slashIdx:]
		} else {
			path = "/"
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	operationID := prefix
	if operationID != "" {
		operationID += "_"
	}
	operationID += sanitizeName(res.Name)
	if operationID == "" {
		operationID = method + "_op"
	}

	toolName := canonical.ToolName(apiName, operationID)

	summary := res.Name
	if res.Description != "" {
		summary = res.Name + ": " + res.Description
	}

	var params []canonical.Parameter
	properties := map[string]any{}
	requiredFields := []string{}

	// Extract path parameters.
	for _, match := range insomniaVarRe.FindAllStringSubmatch(res.URL, -1) {
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

	// Extract query parameters.
	for _, qp := range res.Parameters {
		if qp.Disabled {
			continue
		}
		params = append(params, canonical.Parameter{
			Name:     qp.Name,
			In:       "query",
			Required: false,
			Schema:   map[string]any{"type": "string", "description": qp.Description},
		})
		properties[qp.Name] = map[string]any{"type": "string", "description": qp.Description}
	}

	// Extract headers (skip common ones).
	for _, h := range res.Headers {
		if h.Disabled {
			continue
		}
		lower := strings.ToLower(h.Name)
		if lower == "content-type" || lower == "authorization" || lower == "accept" {
			continue
		}
		params = append(params, canonical.Parameter{
			Name:     h.Name,
			In:       "header",
			Required: false,
			Schema:   map[string]any{"type": "string"},
		})
		properties[h.Name] = map[string]any{"type": "string"}
	}

	// Body.
	var reqBody *canonical.RequestBody
	if res.Body != nil && res.Body.MimeType != "" {
		reqBody = &canonical.RequestBody{
			Required:    true,
			ContentType: res.Body.MimeType,
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

func buildFolderPrefix(parentID string, folderNames map[string]string, resources []Resource) string {
	var parts []string
	seen := map[string]bool{}
	for parentID != "" {
		if seen[parentID] {
			break
		}
		seen[parentID] = true
		name, ok := folderNames[parentID]
		if !ok {
			break
		}
		parts = append([]string{sanitizeName(name)}, parts...)
		// Find parent's parent.
		found := false
		for _, res := range resources {
			if res.ID == parentID {
				parentID = res.ParentID
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	return strings.Join(parts, "_")
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == ' ' || r == '-' || r == '/':
			b.WriteRune('_')
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Insomnia export v4 JSON structures.

type Export struct {
	Type         string     `json:"_type"`
	ExportFormat int        `json:"__export_format"`
	ExportDate   string     `json:"__export_date"`
	ExportSource string     `json:"__export_source"`
	Resources    []Resource `json:"resources"`
}

type Resource struct {
	ID          string         `json:"_id"`
	Type        string         `json:"_type"`
	ParentID    string         `json:"parentId"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	Headers     []Header       `json:"headers"`
	Parameters  []QueryParam   `json:"parameters"`
	Body        *Body          `json:"body"`
	Data        map[string]any `json:"data"`
}

type Header struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

type QueryParam struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
}

type Body struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}
