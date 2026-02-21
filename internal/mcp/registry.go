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
	Annotations  map[string]any
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
				Annotations:  buildAnnotations(op),
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

// buildAnnotations derives MCP tool annotations from the operation's protocol metadata.
// BuildResourceTemplates generates RFC 6570 URI templates from parameterized operation paths.
// Groups by service + resource base path to avoid creating hundreds of templates.
func (r *Registry) BuildResourceTemplates() []map[string]any {
	seen := make(map[string]bool)
	var templates []map[string]any

	for _, tool := range r.SortedTools() {
		op := tool.Operation
		if op.Path == "" || op.RESTComposite != nil {
			continue
		}
		// Only create templates for paths with parameters
		if !strings.Contains(op.Path, "{") {
			continue
		}
		service := op.ServiceName
		if service == "" {
			service = "api"
		}

		// Build URI template: service://path with {params}
		uriTemplate := strings.ToLower(service) + "://" + strings.TrimPrefix(op.Path, "/")
		if seen[uriTemplate] {
			continue
		}
		seen[uriTemplate] = true

		// Human-readable name from the path
		name := service + " " + op.Path

		templates = append(templates, map[string]any{
			"uriTemplate": uriTemplate,
			"name":        name,
			"description": op.Summary,
			"mimeType":    "application/json",
		})
	}
	return templates
}

// buildAnnotations derives MCP tool annotations from the operation's protocol metadata.
func buildAnnotations(op *canonical.Operation) map[string]any {
	readOnly := false
	destructive := false
	idempotent := false

	if op.RESTComposite != nil {
		// Composite tools: merge annotations from all sub-actions (most permissive wins)
		for _, subOp := range op.RESTComposite.Actions {
			sub := methodAnnotations(subOp.Method)
			if !sub.readOnly {
				readOnly = false
			}
			if sub.destructive {
				destructive = true
			}
			// Composite is not idempotent if any sub-action isn't
		}
	} else if op.GraphQL != nil {
		readOnly = op.GraphQL.OperationType == "query"
	} else if op.Protocol == "grpc" || op.SoapNamespace != "" || op.JSONRPC != nil {
		// gRPC, SOAP, JSON-RPC: can't infer safely, use conservative defaults
		readOnly = false
		destructive = false
	} else {
		a := methodAnnotations(op.Method)
		readOnly = a.readOnly
		destructive = a.destructive
		idempotent = a.idempotent
	}

	return map[string]any{
		"readOnlyHint":     readOnly,
		"destructiveHint":  destructive,
		"idempotentHint":   idempotent,
		"openWorldHint":    true,
	}
}

type methodHints struct {
	readOnly, destructive, idempotent bool
}

func methodAnnotations(method string) methodHints {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS":
		return methodHints{readOnly: true, idempotent: true}
	case "PUT":
		return methodHints{idempotent: true}
	case "DELETE":
		return methodHints{destructive: true}
	default: // POST, PATCH, etc.
		return methodHints{}
	}
}

// cleanSummary normalizes auto-generated descriptions from API specs.
func cleanSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Strip verbose preambles
	prefixes := []string{
		"This endpoint ", "This resource represents ", "This method ",
		"This operation ", "This API ", "This service ",
		"Returns a ", "Returns the ", "Returns an ",
		"Retrieve a ", "Retrieve the ", "Retrieve an ",
		"Get a ", "Get the ", "Get an ",
	}
	lower := strings.ToLower(s)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			s = s[len(prefix):]
			// Capitalize first letter
			if len(s) > 0 {
				s = strings.ToUpper(s[:1]) + s[1:]
			}
			break
		}
	}
	// Collapse multiple spaces/newlines
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func buildDescription(op *canonical.Operation) string {
	base := cleanSummary(op.Summary)
	if base == "" {
		base = op.ID
	}
	params := parameterDescriptions(op)
	if len(params) == 0 {
		if len(base) > 300 {
			return base[:297] + "..."
		}
		return base
	}
	desc := base + " Parameters: " + strings.Join(params, "; ")
	if len(desc) > 300 {
		return desc[:297] + "..."
	}
	return desc
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
