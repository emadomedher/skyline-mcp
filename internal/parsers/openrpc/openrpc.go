package openrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"mcp-api-bridge/internal/canonical"
)

// LooksLikeOpenRPC reports whether raw looks like an OpenRPC document.
func LooksLikeOpenRPC(raw []byte) bool {
	var doc struct {
		OpenRPC string `json:"openrpc"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	return doc.OpenRPC != ""
}

// ParseToCanonical parses an OpenRPC document into a canonical Service.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx

	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("openrpc: decode failed: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		// Try servers array.
		for _, s := range doc.Servers {
			if s.URL != "" {
				baseURL = strings.TrimRight(s.URL, "/")
				break
			}
		}
	}
	if baseURL == "" {
		return nil, fmt.Errorf("openrpc: base_url_override is required (or set a server in the OpenRPC document)")
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	for _, method := range doc.Methods {
		op := buildOperation(apiName, method)
		if op != nil {
			service.Operations = append(service.Operations, op)
		}
	}

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("openrpc: no methods found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

func buildOperation(apiName string, method Method) *canonical.Operation {
	if method.Name == "" {
		return nil
	}

	operationID := sanitizeName(method.Name)
	toolName := canonical.ToolName(apiName, operationID)

	summary := method.Summary
	if summary == "" {
		summary = method.Name
	}
	if method.Description != "" && method.Summary != "" {
		summary = method.Summary + ": " + method.Description
	}

	properties := map[string]any{}
	requiredFields := []string{}
	var params []canonical.Parameter

	for _, p := range method.Params {
		schema := p.Schema
		if schema == nil {
			schema = map[string]any{"type": "string"}
		}
		param := canonical.Parameter{
			Name:     p.Name,
			In:       "body",
			Required: p.Required,
			Schema:   schema,
		}
		params = append(params, param)
		properties[p.Name] = schema
		if p.Required {
			requiredFields = append(requiredFields, p.Name)
		}
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":          properties,
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
		Method:      "post",
		Path:        "/",
		Summary:     summary,
		Parameters:  params,
		RequestBody: &canonical.RequestBody{
			Required:    true,
			ContentType: "application/json",
			Schema:      map[string]any{"type": "object", "additionalProperties": true},
		},
		InputSchema: inputSchema,
		JSONRPC: &canonical.JSONRPCOperation{
			MethodName: method.Name,
		},
	}
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '.' || r == '-' || r == ' ' || r == '/':
			b.WriteRune('_')
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// OpenRPC document structures.

type Document struct {
	OpenRPC string   `json:"openrpc"`
	Info    Info     `json:"info"`
	Servers []Server `json:"servers"`
	Methods []Method `json:"methods"`
}

type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type Server struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Method struct {
	Name        string          `json:"name"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Params      []Param         `json:"params"`
	Result      *MethodResult   `json:"result"`
}

type Param struct {
	Name     string         `json:"name"`
	Summary  string         `json:"summary"`
	Required bool           `json:"required"`
	Schema   map[string]any `json:"schema"`
}

type MethodResult struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
}
