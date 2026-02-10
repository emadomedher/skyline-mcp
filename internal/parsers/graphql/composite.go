package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"mcp-api-bridge/internal/canonical"
	gql "mcp-api-bridge/internal/graphql"
)

// generateCompositeTools creates MCP tools from CRUD patterns
func generateCompositeTools(schema *ast.Schema, apiName, baseURL string, patterns []*gql.CRUDPattern) ([]*canonical.Operation, error) {
	var operations []*canonical.Operation

	for _, pattern := range patterns {
		op, err := buildCompositeOperation(schema, apiName, baseURL, pattern)
		if err != nil {
			return nil, fmt.Errorf("build composite for %s: %w", pattern.BaseType, err)
		}
		operations = append(operations, op)
	}

	sort.Slice(operations, func(i, j int) bool {
		return operations[i].ToolName < operations[j].ToolName
	})

	return operations, nil
}

// buildCompositeOperation creates a single composite MCP tool from a CRUD pattern
func buildCompositeOperation(schema *ast.Schema, apiName, baseURL string, pattern *gql.CRUDPattern) (*canonical.Operation, error) {
	baseTypeLower := strings.ToLower(pattern.BaseType)
	toolName := canonical.ToolName(apiName, baseTypeLower+"_manage")

	// Collect all parameters from create, update, and set operations
	params := make([]canonical.Parameter, 0)
	properties := make(map[string]any)
	required := make([]string, 0)
	seenParams := make(map[string]bool)

	// Add 'id' parameter (optional for create, required for update)
	idParam := canonical.Parameter{
		Name:     "id",
		In:       "argument",
		Required: false, // Optional - if missing, create; if present, update
		Schema: map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("ID of the %s to update. Omit to create a new %s.", pattern.BaseType, baseTypeLower),
		},
	}
	params = append(params, idParam)
	properties["id"] = idParam.Schema
	seenParams["id"] = true

	// Extract parameters from create operation
	if pattern.Create != nil {
		for _, arg := range pattern.Create.Arguments {
			if arg == nil || seenParams[arg.Name] {
				continue
			}
			param := canonical.Parameter{
				Name:     arg.Name,
				In:       "argument",
				Required: false, // Make everything optional (smart orchestration will handle)
				Schema:   inputSchemaForType(schema, arg.Type, 0),
			}
			params = append(params, param)
			properties[arg.Name] = param.Schema
			seenParams[arg.Name] = true
		}
	}

	// Extract parameters from update operation
	if pattern.Update != nil {
		for _, arg := range pattern.Update.Arguments {
			if arg == nil || seenParams[arg.Name] {
				continue
			}
			param := canonical.Parameter{
				Name:     arg.Name,
				In:       "argument",
				Required: false,
				Schema:   inputSchemaForType(schema, arg.Type, 0),
			}
			params = append(params, param)
			properties[arg.Name] = param.Schema
			seenParams[arg.Name] = true
		}
	}

	// Extract parameters from set operations (issueSetLabels, issueSetAssignees, etc.)
	for _, setOp := range pattern.SetOps {
		for _, arg := range setOp.Arguments {
			if arg == nil || seenParams[arg.Name] {
				continue
			}
			param := canonical.Parameter{
				Name:     arg.Name,
				In:       "argument",
				Required: false,
				Schema:   inputSchemaForType(schema, arg.Type, 0),
			}
			params = append(params, param)
			properties[arg.Name] = param.Schema
			seenParams[arg.Name] = true
		}
	}

	// Generate description
	capabilities := []string{}
	if pattern.Create != nil {
		capabilities = append(capabilities, "create")
	}
	if pattern.Update != nil {
		capabilities = append(capabilities, "update")
	}
	if pattern.Delete != nil {
		capabilities = append(capabilities, "delete")
	}
	if len(pattern.SetOps) > 0 {
		capabilities = append(capabilities, fmt.Sprintf("set properties (%d operations)", len(pattern.SetOps)))
	}

	description := fmt.Sprintf(
		"Manage %s entities. Supports: %s. This composite tool automatically orchestrates multiple GraphQL mutations based on which parameters are provided.",
		pattern.BaseType,
		strings.Join(capabilities, ", "),
	)

	// Build input schema for MCP
	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		inputSchema["required"] = required
	}

	// Build operation
	op := &canonical.Operation{
		ServiceName: apiName, // CRITICAL: needed for service registry lookup
		ToolName:    toolName,
		ID:          baseTypeLower + "_manage",
		Summary:     fmt.Sprintf("Manage %s (composite)", pattern.BaseType),
		Description: description,
		Parameters:  params,
		RequestBody: &canonical.RequestBody{
			Required: true,
			Content: map[string]canonical.MediaType{
				"application/json": {
					Schema: map[string]any{
						"type":       "object",
						"properties": properties,
						"required":   required,
					},
				},
			},
		},
		InputSchema: inputSchema,
		Method:      "POST",
		HTTPMethod:  "POST",
		Path:        baseURL,
		ContentType: "application/json",
		GraphQL: &canonical.GraphQLOperation{
			OperationType: "mutation",
			FieldName:     baseTypeLower + "_composite", // Placeholder - orchestration will handle
			Composite: &canonical.GraphQLComposite{
				Pattern:  pattern.BaseType,
				Create:   fieldToOpRef(pattern.Create),
				Update:   fieldToOpRef(pattern.Update),
				Delete:   fieldToOpRef(pattern.Delete),
				SetOps:   fieldsToOpRefs(pattern.SetOps),
			},
		},
	}

	return op, nil
}

// fieldToOpRef converts a FieldDefinition to an operation reference
func fieldToOpRef(field *ast.FieldDefinition) *canonical.GraphQLOpRef {
	if field == nil {
		return nil
	}
	return &canonical.GraphQLOpRef{
		Name: field.Name,
		Type: "mutation",
	}
}

// fieldsToOpRefs converts multiple FieldDefinitions to operation references
func fieldsToOpRefs(fields []*ast.FieldDefinition) []*canonical.GraphQLOpRef {
	refs := make([]*canonical.GraphQLOpRef, 0, len(fields))
	for _, field := range fields {
		if field != nil {
			refs = append(refs, &canonical.GraphQLOpRef{
				Name: field.Name,
				Type: "mutation",
			})
		}
	}
	return refs
}
