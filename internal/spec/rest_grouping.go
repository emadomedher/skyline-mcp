package spec

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
)

// ApplyRESTGrouping applies REST CRUD grouping to services that opt in.
// Auto-enabled for well-known APIs (Jira, Slack) and any API with optimization.enable_crud_grouping.
func ApplyRESTGrouping(services []*canonical.Service, apiConfigs []config.APIConfig, logger *log.Logger) []*canonical.Service {
	// Build lookup of which APIs should have REST grouping
	shouldGroup := make(map[string]bool)
	for _, api := range apiConfigs {
		if api.Optimization != nil && api.Optimization.EnableCRUDGrouping {
			shouldGroup[api.Name] = true
		}
		// Auto-enable for well-known REST APIs with high tool counts
		nameL := strings.ToLower(api.Name)
		if strings.Contains(nameL, "jira") || strings.Contains(nameL, "slack") || strings.Contains(nameL, "gitlab") {
			shouldGroup[api.Name] = true
		}
	}

	result := make([]*canonical.Service, 0, len(services))
	for _, svc := range services {
		if !shouldGroup[svc.Name] {
			result = append(result, svc)
			continue
		}

		before := len(svc.Operations)
		grouped := GroupRESTOperations(svc.Operations, svc.Name)
		after := len(grouped)

		if after < before {
			logger.Printf("REST grouping for %s: %d operations → %d tools (%.0f%% reduction)",
				svc.Name, before, after, float64(before-after)/float64(before)*100)
		}

		result = append(result, &canonical.Service{
			Name:       svc.Name,
			BaseURL:    svc.BaseURL,
			Operations: grouped,
		})
	}

	return result
}

// GroupRESTOperations groups REST operations by resource path into composite tools.
// Operations sharing the same resource get merged into a single tool with an "action" parameter.
// Groups with only 1 operation are kept as standalone tools.
func GroupRESTOperations(ops []*canonical.Operation, apiName string) []*canonical.Operation {
	// Group operations by resource key
	groups := make(map[string][]*canonical.Operation)
	groupOrder := make([]string, 0)
	for _, op := range ops {
		// Skip non-REST operations (GraphQL, gRPC, etc.)
		if op.GraphQL != nil || op.Protocol == "grpc" || op.JSONRPC != nil || op.RESTComposite != nil {
			continue
		}
		key := computeResourceKey(op.Path)
		if key == "" {
			key = "__standalone__" + op.ID
		}
		if _, exists := groups[key]; !exists {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], op)
	}

	// Collect non-REST ops to pass through unchanged
	var result []*canonical.Operation
	for _, op := range ops {
		if op.GraphQL != nil || op.Protocol == "grpc" || op.JSONRPC != nil || op.RESTComposite != nil {
			result = append(result, op)
		}
	}

	// Process each group
	sort.Strings(groupOrder)
	for _, key := range groupOrder {
		group := groups[key]
		if len(group) < 2 {
			// Single operation — keep as standalone
			result = append(result, group[0])
			continue
		}

		composite, err := buildRESTComposite(group, apiName, key)
		if err != nil {
			// If grouping fails, keep individual operations
			result = append(result, group...)
			continue
		}
		result = append(result, composite)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ToolName < result[j].ToolName
	})

	return result
}

// computeResourceKey extracts a grouping key from an operation's path.
// REST-style: strips trailing path parameters → /issues/{id} becomes /issues
// RPC-style: extracts dot-prefix → /conversations.list becomes conversations
func computeResourceKey(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}

	segments := strings.Split(path, "/")
	lastSeg := segments[len(segments)-1]

	// RPC-style: last segment contains a dot (e.g., "conversations.list")
	if dotIdx := strings.Index(lastSeg, "."); dotIdx > 0 {
		prefix := lastSeg[:dotIdx]
		if len(segments) > 1 {
			return strings.Join(segments[:len(segments)-1], "/") + "/" + prefix
		}
		return prefix
	}

	// REST-style: strip trailing path parameter segments
	for len(segments) > 0 && isPathParam(segments[len(segments)-1]) {
		segments = segments[:len(segments)-1]
	}

	if len(segments) == 0 {
		return ""
	}

	return strings.Join(segments, "/")
}

// isPathParam returns true if the segment is a path parameter like {id} or {issueIdOrKey}
func isPathParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

// buildRESTComposite creates a single composite tool from a group of related operations.
func buildRESTComposite(group []*canonical.Operation, apiName, resourceKey string) (*canonical.Operation, error) {
	if len(group) == 0 {
		return nil, fmt.Errorf("empty group")
	}

	resourceName := extractResourceName(resourceKey)
	actions := make(map[string]*canonical.Operation)
	actionNames := make([]string, 0, len(group))

	for _, op := range group {
		action := computeActionName(op)
		// Handle duplicate action names by appending method
		if _, exists := actions[action]; exists {
			action = action + "_" + strings.ToLower(op.Method)
		}
		actions[action] = op
		actionNames = append(actionNames, action)
	}
	sort.Strings(actionNames)

	// Build merged parameters: action enum + union of all sub-op parameters
	properties := make(map[string]any)
	mergedParams := []canonical.Parameter{}
	seenParams := make(map[string]bool)

	// Add "action" parameter
	actionSchema := map[string]any{
		"type":        "string",
		"enum":        actionNames,
		"description": fmt.Sprintf("Operation to perform on %s", resourceName),
	}
	properties["action"] = actionSchema
	mergedParams = append(mergedParams, canonical.Parameter{
		Name:     "action",
		In:       "action",
		Required: true,
		Schema:   actionSchema,
	})
	seenParams["action"] = true

	// Merge parameters from all sub-operations
	for _, actionName := range actionNames {
		op := actions[actionName]
		for _, param := range op.Parameters {
			if seenParams[param.Name] {
				continue
			}
			// All params become optional in the composite (only required for their specific action)
			mergedParam := canonical.Parameter{
				Name:     param.Name,
				In:       param.In,
				Required: false,
				Schema:   param.Schema,
			}
			mergedParams = append(mergedParams, mergedParam)
			if param.Schema != nil {
				properties[param.Name] = param.Schema
			} else {
				properties[param.Name] = map[string]any{"type": "string"}
			}
			seenParams[param.Name] = true
		}
		// Include request body fields as parameters too
		if op.RequestBody != nil && op.RequestBody.Schema != nil {
			if props, ok := op.RequestBody.Schema["properties"].(map[string]any); ok {
				for name, schema := range props {
					if seenParams[name] {
						continue
					}
					mergedParams = append(mergedParams, canonical.Parameter{
						Name:     name,
						In:       "body",
						Required: false,
						Schema:   toStringMap(schema),
					})
					properties[name] = schema
					seenParams[name] = true
				}
			}
		}
	}

	// Build description listing all available actions
	var descParts []string
	for _, name := range actionNames {
		op := actions[name]
		summary := op.Summary
		if summary == "" {
			summary = fmt.Sprintf("%s %s", strings.ToUpper(op.Method), op.Path)
		}
		descParts = append(descParts, fmt.Sprintf("- %s: %s", name, summary))
	}
	description := fmt.Sprintf("Manage %s resources. Available actions:\n%s", resourceName, strings.Join(descParts, "\n"))

	// Build input schema
	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             []string{"action"},
		"additionalProperties": false,
	}

	toolName := canonical.ToolName(apiName, resourceName+"_manage")

	return &canonical.Operation{
		ServiceName: group[0].ServiceName,
		ID:          resourceName + "_manage",
		ToolName:    toolName,
		Summary:     fmt.Sprintf("Manage %s (composite)", resourceName),
		Description: description,
		Parameters:  mergedParams,
		InputSchema: inputSchema,
		RESTComposite: &canonical.RESTComposite{
			ResourceName: resourceName,
			Actions:      actions,
		},
	}, nil
}

// computeActionName determines the action name for an operation in a composite group.
func computeActionName(op *canonical.Operation) string {
	path := strings.Trim(op.Path, "/")
	segments := strings.Split(path, "/")
	lastSeg := ""
	if len(segments) > 0 {
		lastSeg = segments[len(segments)-1]
	}

	// RPC-style: extract suffix after dot (e.g., "conversations.list" → "list")
	if dotIdx := strings.Index(lastSeg, "."); dotIdx > 0 {
		return lastSeg[dotIdx+1:]
	}

	// REST-style: determine from HTTP method + path shape
	hasTrailingParam := isPathParam(lastSeg)
	method := strings.ToUpper(op.Method)

	switch method {
	case "GET":
		if hasTrailingParam {
			return "get"
		}
		return "list"
	case "POST":
		return "create"
	case "PUT":
		if hasTrailingParam {
			return "update"
		}
		return "replace"
	case "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		// Fallback: use the operation ID if available
		if op.ID != "" {
			return sanitizeActionName(op.ID)
		}
		return strings.ToLower(method)
	}
}

// extractResourceName returns a human-readable resource name from a resource key.
func extractResourceName(key string) string {
	parts := strings.Split(key, "/")
	// Take the last meaningful segment
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part != "" && !isPathParam(part) && !isAPIVersionSegment(part) {
			return part
		}
	}
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "resource"
}

// isAPIVersionSegment returns true for common API version path segments.
func isAPIVersionSegment(seg string) bool {
	lower := strings.ToLower(seg)
	switch lower {
	case "api", "rest", "v1", "v2", "v3", "v4", "1", "2", "3", "4":
		return true
	}
	return false
}

// sanitizeActionName cleans an operation ID for use as an action name.
func sanitizeActionName(id string) string {
	// Strip common prefixes that are redundant in a composite context
	lower := strings.ToLower(id)
	for _, prefix := range []string{"get_", "post_", "put_", "patch_", "delete_"} {
		if strings.HasPrefix(lower, prefix) {
			return id[len(prefix):]
		}
	}
	return id
}

// toStringMap converts any to map[string]any, returning an empty map for non-map types.
func toStringMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{"type": "string"}
}
