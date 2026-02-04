package googleapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"mcp-api-bridge/internal/canonical"
)

// LooksLikeDiscovery reports whether payload appears to be a Google API Discovery document.
func LooksLikeDiscovery(raw []byte) bool {
	var payload struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(payload.Kind), "discovery#")
}

// ParseToCanonical parses a Google API Discovery document into a canonical Service.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx
	var doc DiscoveryDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("google discovery: parse failed: %w", err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		if doc.BaseURL != "" {
			baseURL = strings.TrimRight(doc.BaseURL, "/")
		} else if doc.RootURL != "" || doc.ServicePath != "" {
			baseURL = strings.TrimRight(doc.RootURL+doc.ServicePath, "/")
		}
	}
	if baseURL == "" {
		return nil, fmt.Errorf("google discovery: base URL missing")
	}

	resolver := newSchemaResolver(doc.Schemas)
	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	var entries []methodEntry
	if len(doc.Methods) > 0 {
		entries = append(entries, collectMethods("", doc.Methods)...)
	}
	for name, res := range doc.Resources {
		entries = append(entries, collectResourceMethods(name, res)...)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("google discovery: no methods found")
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].FullName < entries[j].FullName
	})

	for _, entry := range entries {
		op, err := buildOperation(&doc, entry, apiName, resolver)
		if err != nil {
			return nil, err
		}
		service.Operations = append(service.Operations, op)
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

type methodEntry struct {
	FullName string
	Name     string
	Method   *DiscoveryMethod
}

func collectResourceMethods(prefix string, res *DiscoveryResource) []methodEntry {
	var entries []methodEntry
	if res == nil {
		return entries
	}
	resourceNames := make([]string, 0, len(res.Resources))
	for name := range res.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	methodNames := make([]string, 0, len(res.Methods))
	for name := range res.Methods {
		methodNames = append(methodNames, name)
	}
	sort.Strings(methodNames)

	for _, name := range methodNames {
		full := prefix + "." + name
		if prefix == "" {
			full = name
		}
		entries = append(entries, methodEntry{
			FullName: full,
			Name:     name,
			Method:   res.Methods[name],
		})
	}

	for _, name := range resourceNames {
		nextPrefix := name
		if prefix != "" {
			nextPrefix = prefix + "." + name
		}
		entries = append(entries, collectResourceMethods(nextPrefix, res.Resources[name])...)
	}
	return entries
}

func collectMethods(prefix string, methods map[string]*DiscoveryMethod) []methodEntry {
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	var entries []methodEntry
	for _, name := range names {
		full := prefix + "." + name
		if prefix == "" {
			full = name
		}
		entries = append(entries, methodEntry{
			FullName: full,
			Name:     name,
			Method:   methods[name],
		})
	}
	return entries
}

func buildOperation(doc *DiscoveryDoc, entry methodEntry, apiName string, resolver *schemaResolver) (*canonical.Operation, error) {
	method := entry.Method
	if method == nil {
		return nil, fmt.Errorf("google discovery: nil method for %s", entry.FullName)
	}

	operationID := resolveOperationID(doc, entry)
	toolName := canonical.ToolName(apiName, operationID)
	path := "/" + strings.TrimLeft(method.Path, "/")

	parameters := mergeParameters(doc.Parameters, method.Parameters)
	paramKeys := make([]string, 0, len(parameters))
	for name := range parameters {
		paramKeys = append(paramKeys, name)
	}
	sort.Strings(paramKeys)

	properties := map[string]any{}
	required := []string{}
	var params []canonical.Parameter
	for _, name := range paramKeys {
		param := parameters[name]
		if param == nil {
			continue
		}
		location := strings.ToLower(param.Location)
		if location == "" {
			location = "query"
		}
		if location != "path" && location != "query" && location != "header" {
			continue
		}
		schema := paramSchema(param)
		properties[name] = schema
		if param.Required {
			required = append(required, name)
		}
		params = append(params, canonical.Parameter{
			Name:     name,
			In:       location,
			Required: param.Required,
			Schema:   schema,
		})
	}

	var requestBody *canonical.RequestBody
	if method.Request != nil {
		bodySchema := resolver.ResolveRef(method.Request)
		if method.Request.Description != "" {
			bodySchema["description"] = method.Request.Description
		}
		requestBody = &canonical.RequestBody{
			Required:    requiresBody(method.HTTPMethod),
			ContentType: "application/json",
			Schema:      bodySchema,
		}
		properties["body"] = bodySchema
		if requestBody.Required {
			required = append(required, "body")
		}
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		inputSchema["required"] = uniqueSorted(required)
	}

	var responseSchema map[string]any
	if method.Response != nil {
		responseSchema = resolver.ResolveRef(method.Response)
	}

	summary := strings.TrimSpace(method.Description)
	if summary == "" {
		summary = operationID
	}

	return &canonical.Operation{
		ServiceName:    apiName,
		ID:             operationID,
		ToolName:       toolName,
		Method:         strings.ToLower(method.HTTPMethod),
		Path:           path,
		Summary:        summary,
		Parameters:     params,
		RequestBody:    requestBody,
		InputSchema:    inputSchema,
		ResponseSchema: responseSchema,
	}, nil
}

func resolveOperationID(doc *DiscoveryDoc, entry methodEntry) string {
	if entry.Method != nil && entry.Method.ID != "" {
		id := entry.Method.ID
		if doc.Name != "" && strings.HasPrefix(id, doc.Name+".") {
			return strings.TrimPrefix(id, doc.Name+".")
		}
		return id
	}
	if entry.FullName != "" {
		return entry.FullName
	}
	if entry.Method != nil && entry.Method.Path != "" {
		return strings.ToLower(entry.Method.HTTPMethod) + "_" + strings.ReplaceAll(entry.Method.Path, "/", "_")
	}
	return entry.Name
}

func requiresBody(method string) bool {
	switch strings.ToUpper(method) {
	case "GET", "DELETE", "HEAD":
		return false
	default:
		return true
	}
}

func mergeParameters(global, local map[string]*DiscoveryParam) map[string]*DiscoveryParam {
	merged := map[string]*DiscoveryParam{}
	for name, param := range global {
		merged[name] = param
	}
	for name, param := range local {
		merged[name] = param
	}
	return merged
}

func paramSchema(param *DiscoveryParam) map[string]any {
	baseType := param.Type
	if baseType == "" {
		baseType = "string"
	}
	schema := map[string]any{}
	if param.Repeated {
		schema["type"] = "array"
		schema["items"] = map[string]any{"type": baseType}
	} else if baseType == "array" {
		schema["type"] = "array"
		if param.Items != nil {
			schema["items"] = param.Items
		}
	} else {
		schema["type"] = baseType
	}
	if param.Description != "" {
		schema["description"] = param.Description
	}
	if len(param.Enum) > 0 {
		schema["enum"] = param.Enum
	}
	if param.Format != "" {
		schema["format"] = param.Format
	}
	return schema
}

type schemaResolver struct {
	defs map[string]*DiscoverySchema
}

func newSchemaResolver(defs map[string]*DiscoverySchema) *schemaResolver {
	if defs == nil {
		defs = map[string]*DiscoverySchema{}
	}
	return &schemaResolver{defs: defs}
}

func (r *schemaResolver) ResolveRef(ref *SchemaRef) map[string]any {
	if ref == nil {
		return map[string]any{"type": "object"}
	}
	return r.resolve(&DiscoverySchema{Ref: ref.Ref}, map[string]bool{}, 0)
}

func (r *schemaResolver) resolve(schema *DiscoverySchema, seen map[string]bool, depth int) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object"}
	}
	if depth > 10 {
		return map[string]any{"type": "object"}
	}

	if schema.Ref != "" {
		if seen[schema.Ref] {
			return map[string]any{"type": "object"}
		}
		seen[schema.Ref] = true
		if def, ok := r.defs[schema.Ref]; ok {
			return r.resolve(def, seen, depth+1)
		}
		return map[string]any{"type": "object"}
	}

	baseType := schema.Type
	if baseType == "" && len(schema.Properties) > 0 {
		baseType = "object"
	}
	if baseType == "" {
		baseType = "object"
	}

	out := map[string]any{"type": baseType}
	if schema.Description != "" {
		out["description"] = schema.Description
	}
	if len(schema.Enum) > 0 {
		out["enum"] = schema.Enum
	}
	if schema.Format != "" {
		out["format"] = schema.Format
	}

	switch baseType {
	case "array":
		out["items"] = r.resolve(schema.Items, seen, depth+1)
	case "object":
		props := map[string]any{}
		names := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			props[name] = r.resolve(schema.Properties[name], seen, depth+1)
		}
		if len(props) > 0 {
			out["properties"] = props
		}
		if len(schema.Required) > 0 {
			out["required"] = uniqueSorted(schema.Required)
		}
		if schema.AdditionalProperties != nil {
			out["additionalProperties"] = r.resolve(schema.AdditionalProperties, seen, depth+1)
		}
	}

	if schema.Repeated {
		return map[string]any{
			"type":  "array",
			"items": out,
		}
	}
	if baseType == "any" {
		return map[string]any{}
	}
	return out
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, v := range values {
		if v != "" {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
