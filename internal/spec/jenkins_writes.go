package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
)

var pathParamName = regexp.MustCompile(`\{([^}]+)\}`)

func appendJenkinsWrites(service *canonical.Service, api config.APIConfig) error {
	if service == nil {
		return fmt.Errorf("missing service")
	}
	for _, write := range api.Jenkins.AllowWrites {
		op, err := buildJenkinsWriteOperation(api.Name, write)
		if err != nil {
			return err
		}
		if _, exists := findOperation(service.Operations, op.ToolName); exists {
			return fmt.Errorf("duplicate tool name %s", op.ToolName)
		}
		service.Operations = append(service.Operations, op)
	}
	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})
	return nil
}

func buildJenkinsWriteOperation(apiName string, write config.JenkinsWrite) (*canonical.Operation, error) {
	method := strings.ToLower(strings.TrimSpace(write.Method))
	if method == "" {
		return nil, fmt.Errorf("missing method")
	}
	path := strings.TrimSpace(write.Path)
	if path == "" {
		return nil, fmt.Errorf("missing path")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	operationID := write.Name
	if operationID == "" {
		operationID = fmt.Sprintf("%s_%s", method, strings.Trim(path, "/"))
		operationID = strings.ReplaceAll(operationID, "/", "_")
	}

	params := []canonical.Parameter{}
	properties := map[string]any{}
	required := []string{}
	placeholders := pathParamName.FindAllStringSubmatch(path, -1)
	for _, match := range placeholders {
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		if _, ok := properties[name]; ok {
			continue
		}
		schema := map[string]any{"type": "string", "description": "Jenkins path parameter."}
		params = append(params, canonical.Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   schema,
		})
		properties[name] = schema
		required = append(required, name)
	}

	properties["parameters"] = map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"description":          "Query parameters for Jenkins buildWithParameters (optional).",
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		inputSchema["required"] = uniqueSorted(required)
	}

	summary := strings.TrimSpace(write.Summary)
	if summary == "" {
		if strings.HasSuffix(strings.ToLower(path), "/buildwithparameters") {
			summary = "Trigger a Jenkins job with parameters."
		} else if strings.HasSuffix(strings.ToLower(path), "/build") {
			summary = "Trigger a Jenkins job build."
		} else {
			summary = "Jenkins write operation."
		}
	}
	if strings.HasSuffix(strings.ToLower(path), "/buildwithparameters") {
		summary += " Use parameters object for query params."
	}

	return &canonical.Operation{
		ServiceName:       apiName,
		ID:                operationID,
		ToolName:          canonical.ToolName(apiName, operationID),
		Method:            method,
		Path:              path,
		Summary:           summary,
		Parameters:        params,
		RequestBody:       nil,
		InputSchema:       inputSchema,
		ResponseSchema:    nil,
		QueryParamsObject: "parameters",
		RequiresCrumb:     true,
	}, nil
}

func findOperation(ops []*canonical.Operation, toolName string) (*canonical.Operation, bool) {
	for _, op := range ops {
		if op.ToolName == toolName {
			return op, true
		}
	}
	return nil, false
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
