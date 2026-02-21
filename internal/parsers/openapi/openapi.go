package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"

	"skyline-mcp/internal/canonical"
)

func LooksLikeOpenAPI(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	return strings.Contains(lower, "openapi:") || strings.Contains(lower, "\"openapi\"")
}

func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromData(raw)
	if err != nil {
		return nil, err
	}

	opts := []openapi3.ValidationOption{
		openapi3.DisableExamplesValidation(),
		openapi3.DisableSchemaDefaultsValidation(),
	}

	// Validate but don't fail hard — many real-world specs (GitLab, etc.) have
	// minor validation issues (e.g. arrays without items) but are perfectly usable.
	if err := doc.Validate(ctx, opts...); err != nil {
		sanitized, serr := sanitizeExamples(raw)
		if serr == nil {
			if doc2, lerr := loader.LoadFromData(sanitized); lerr == nil {
				if doc2.Validate(ctx, opts...) == nil {
					doc = doc2
				}
			}
		}
		// Proceed with the loaded doc even if validation failed.
	}

	baseURL := strings.TrimRight(baseURLOverride, "/")
	if baseURL == "" && len(doc.Servers) > 0 {
		baseURL = strings.TrimRight(doc.Servers[0].URL, "/")
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	pathKeys := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		pathKeys = append(pathKeys, path)
	}
	sort.Strings(pathKeys)

	for _, path := range pathKeys {
		item := doc.Paths.Find(path)
		if item == nil {
			continue
		}
		ops := collectOperations(item)
		methodKeys := make([]string, 0, len(ops))
		for method := range ops {
			methodKeys = append(methodKeys, method)
		}
		sort.Strings(methodKeys)
		for _, method := range methodKeys {
			op := ops[method]
			operation := buildOperation(apiName, path, method, item, op)
			service.Operations = append(service.Operations, operation)
		}
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

func sanitizeExamples(raw []byte) ([]byte, error) {
	var payload any
	if err := yaml.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	clean := removeExampleFields(payload)
	normalized := normalizeYAML(clean)
	return json.Marshal(normalized)
}

func removeExampleFields(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, val := range v {
			if key == "example" || key == "examples" {
				continue
			}
			out[key] = removeExampleFields(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, val := range v {
			ks, ok := key.(string)
			if !ok {
				continue
			}
			if ks == "example" || ks == "examples" {
				continue
			}
			out[ks] = removeExampleFields(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, removeExampleFields(item))
		}
		return out
	default:
		return v
	}
}

func normalizeYAML(value any) any {
	switch v := value.(type) {
	case map[any]any:
		out := map[string]any{}
		for key, val := range v {
			ks, ok := key.(string)
			if !ok {
				continue
			}
			out[ks] = normalizeYAML(val)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, val := range v {
			out[key] = normalizeYAML(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeYAML(item))
		}
		return out
	default:
		return v
	}
}

func collectOperations(item *openapi3.PathItem) map[string]*openapi3.Operation {
	ops := map[string]*openapi3.Operation{}
	if item.Get != nil {
		ops["get"] = item.Get
	}
	if item.Put != nil {
		ops["put"] = item.Put
	}
	if item.Post != nil {
		ops["post"] = item.Post
	}
	if item.Delete != nil {
		ops["delete"] = item.Delete
	}
	if item.Patch != nil {
		ops["patch"] = item.Patch
	}
	if item.Head != nil {
		ops["head"] = item.Head
	}
	if item.Options != nil {
		ops["options"] = item.Options
	}
	return ops
}

func buildOperation(apiName, path, method string, item *openapi3.PathItem, op *openapi3.Operation) *canonical.Operation {
	operationID := op.OperationID
	if operationID == "" {
		operationID = normalizeOperationID(method, path)
	}
	toolName := canonical.ToolName(apiName, operationID)

	parameters := mergeParameters(item.Parameters, op.Parameters)
	params := make([]canonical.Parameter, 0, len(parameters))

	properties := map[string]any{}
	required := []string{}

	for _, param := range parameters {
		if param == nil || param.Value == nil {
			continue
		}
		p := param.Value
		if isAuthHeader(p.In, p.Name) {
			continue
		}
		paramSchema := schemaToMap(p.Schema)
		if p.Content != nil {
			if media := p.Content.Get("application/json"); media != nil {
				paramSchema = schemaToMap(media.Schema)
			}
		}
		if p.Description != "" {
			paramSchema["description"] = p.Description
		}
		requiredParam := p.Required || p.In == "path"
		params = append(params, canonical.Parameter{
			Name:     p.Name,
			In:       p.In,
			Required: requiredParam,
			Schema:   paramSchema,
		})
		properties[p.Name] = paramSchema
		if requiredParam {
			required = append(required, p.Name)
		}
	}

	var requestBody *canonical.RequestBody
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		body := op.RequestBody.Value
		if media := body.Content.Get("application/json"); media != nil {
			requestBody = &canonical.RequestBody{
				Required:    body.Required,
				ContentType: "application/json",
				Schema:      schemaToMap(media.Schema),
			}
			if body.Description != "" {
				requestBody.Schema["description"] = body.Description
			}
			properties["body"] = requestBody.Schema
			if body.Required {
				required = append(required, "body")
			}
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

	return &canonical.Operation{
		ServiceName:    apiName,
		ID:             operationID,
		ToolName:       toolName,
		Method:         method,
		Path:           path,
		Summary:        strings.TrimSpace(op.Summary),
		Parameters:     params,
		RequestBody:    requestBody,
		InputSchema:    inputSchema,
		ResponseSchema: extractResponseSchema(op),
	}
}

func mergeParameters(pathParams, opParams openapi3.Parameters) openapi3.Parameters {
	combined := openapi3.Parameters{}
	combined = append(combined, pathParams...)
	combined = append(combined, opParams...)
	return combined
}

func isAuthHeader(in, name string) bool {
	n := strings.ToLower(name)
	switch in {
	case "header":
		switch n {
		case "authorization", "x-api-key", "api-key", "apikey", "private-token":
			return true
		}
	case "query":
		switch n {
		case "token", "api_key", "apikey", "access_token", "oauth_token":
			return true
		}
	}
	return false
}

func schemaToMap(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil || ref.Value == nil {
		return map[string]any{"type": "string"}
	}
	data, err := json.Marshal(ref.Value)
	if err != nil {
		return map[string]any{"type": "string"}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{"type": "string"}
	}
	return out
}

func normalizeOperationID(method, path string) string {
	clean := strings.ToLower(method + "_" + path)
	clean = strings.ReplaceAll(clean, "/", "_")
	clean = strings.ReplaceAll(clean, "{", "")
	clean = strings.ReplaceAll(clean, "}", "")
	clean = strings.ReplaceAll(clean, "-", "_")
	clean = strings.ReplaceAll(clean, ".", "_")
	clean = strings.Trim(clean, "_")
	return clean
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, v := range values {
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func extractResponseSchema(op *openapi3.Operation) map[string]any {
	if op.Responses == nil {
		return nil
	}
	responses := op.Responses
	statusKeys := make([]int, 0)
	for code := range responses {
		if len(code) >= 3 && code[0] == '2' {
			if val, err := parseStatus(code); err == nil {
				statusKeys = append(statusKeys, val)
			}
		}
	}
	sort.Ints(statusKeys)
	if len(statusKeys) > 0 {
		code := fmt.Sprintf("%d", statusKeys[0])
		if ref := responses[code]; ref != nil && ref.Value != nil {
			if media := ref.Value.Content.Get("application/json"); media != nil {
				return schemaToMap(media.Schema)
			}
		}
	}
	if ref := responses["default"]; ref != nil && ref.Value != nil {
		if media := ref.Value.Content.Get("application/json"); media != nil {
			return schemaToMap(media.Schema)
		}
	}
	return nil
}

func parseStatus(code string) (int, error) {
	var v int
	_, err := fmt.Sscanf(code, "%d", &v)
	return v, err
}
