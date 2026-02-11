package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"skyline-mcp/internal/canonical"
)

type Tool struct {
	Name         string
	Description  string
	InputSchema  map[string]any
	OutputSchema map[string]any
	Operation    *canonical.Operation
	Validator    *jsonschema.Schema
}

type Resource struct {
	URI         string
	Name        string
	MimeType    string
	Description string
	ToolName    string
}

type Registry struct {
	Tools     map[string]*Tool
	Resources map[string]*Resource
}

func NewRegistry(services []*canonical.Service) (*Registry, error) {
	registry := &Registry{
		Tools:     map[string]*Tool{},
		Resources: map[string]*Resource{},
	}
	for _, svc := range services {
		for _, op := range svc.Operations {
			validator, err := compileSchema(op.InputSchema)
			if err != nil {
				// Best-effort: keep tool registration even if schema compilation fails.
				validator = nil
			}
			tool := &Tool{
				Name:         op.ToolName,
				Description:  buildDescription(op),
				InputSchema:  op.InputSchema,
				OutputSchema: outputSchema(op.ResponseSchema),
				Operation:    op,
				Validator:    validator,
			}
			registry.Tools[tool.Name] = tool
			resource := &Resource{
				URI:         fmt.Sprintf("api://%s/%s", svc.Name, op.ID),
				Name:        tool.Name,
				MimeType:    "application/json",
				Description: op.Summary,
				ToolName:    tool.Name,
			}
			registry.Resources[resource.URI] = resource
		}
	}
	return registry, nil
}

// NewRegistryFromGatewayTools creates a registry from gateway tool definitions
// Used in gateway mode where we don't have full canonical.Service objects
func NewRegistryFromGatewayTools(gatewayTools []GatewayTool) *Registry {
	registry := &Registry{
		Tools:     make(map[string]*Tool),
		Resources: make(map[string]*Resource),
	}

	for _, gt := range gatewayTools {
		// Create a minimal operation with just the tool name
		// This allows the gatewayExecutor to identify which tool to call
		minimalOp := &canonical.Operation{
			ToolName: gt.Name,
		}

		registry.Tools[gt.Name] = &Tool{
			Name:         gt.Name,
			Description:  gt.Description,
			InputSchema:  gt.InputSchema,
			OutputSchema: gt.OutputSchema,
			Operation:    minimalOp,
			Validator:    nil, // Schema validation happens on the gateway side
		}
	}

	return registry
}

// GatewayTool represents a tool definition from the gateway
type GatewayTool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`
}

func outputSchema(bodySchema map[string]any) map[string]any {
	body := bodySchema
	if body == nil {
		body = map[string]any{}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "integer"},
			"content_type": map[string]any{"type": "string"},
			"body": body,
		},
		"required": []string{"status", "content_type", "body"},
	}
}

func compileSchema(schema map[string]any) (*jsonschema.Schema, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return compiler.Compile("schema.json")
}

func (r *Registry) SortedTools() []*Tool {
	tools := make([]*Tool, 0, len(r.Tools))
	for _, tool := range r.Tools {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func (r *Registry) SortedResources() []*Resource {
	resources := make([]*Resource, 0, len(r.Resources))
	for _, res := range r.Resources {
		resources = append(resources, res)
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].URI < resources[j].URI })
	return resources
}

func buildDescription(op *canonical.Operation) string {
	base := strings.TrimSpace(op.Summary)
	if base == "" {
		base = op.ID
	}
	params := parameterDescriptions(op)
	if len(params) == 0 {
		return base
	}
	return base + " Parameters: " + strings.Join(params, "; ")
}

func parameterDescriptions(op *canonical.Operation) []string {
	entries := []string{}
	for _, param := range op.Parameters {
		entry := fmt.Sprintf("%s (%s, %s, %s)", param.Name, param.In, requiredLabel(param.Required), schemaType(param.Schema))
		if desc, ok := param.Schema["description"].(string); ok && strings.TrimSpace(desc) != "" {
			entry += " - " + strings.TrimSpace(desc)
		}
		entries = append(entries, entry)
	}
	if op.RequestBody != nil {
		bodyType := "json"
		if op.SoapNamespace != "" || (op.RequestBody.ContentType != "" && !strings.Contains(op.RequestBody.ContentType, "json")) {
			bodyType = "string"
		}
		entry := fmt.Sprintf("body (%s, %s)", bodyType, requiredLabel(op.RequestBody.Required))
		if desc, ok := op.RequestBody.Schema["description"].(string); ok && strings.TrimSpace(desc) != "" {
			entry += " - " + strings.TrimSpace(desc)
		}
		entries = append(entries, entry)
	}
	if op.SoapNamespace != "" {
		entries = append(entries, "parameters (object, optional) - SOAP key/value params")
	}
	if op.QueryParamsObject != "" {
		entries = append(entries, fmt.Sprintf("%s (object, optional) - query parameters", op.QueryParamsObject))
	}
	if len(entries) > 8 {
		entries = append(entries[:8], fmt.Sprintf("... and %d more", len(entries)-8))
	}
	return entries
}

func requiredLabel(required bool) string {
	if required {
		return "required"
	}
	return "optional"
}

func schemaType(schema map[string]any) string {
	if schema == nil {
		return "object"
	}
	switch t := schema["type"].(type) {
	case string:
		if t == "array" {
			return "array<" + schemaItemType(schema) + ">"
		}
		return t
	case []any:
		parts := []string{}
		for _, v := range t {
			if s, ok := v.(string); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "|")
		}
	}
	return "object"
}

func schemaItemType(schema map[string]any) string {
	if items, ok := schema["items"].(map[string]any); ok {
		if t, ok := items["type"].(string); ok && t != "" {
			return t
		}
	}
	return "object"
}
