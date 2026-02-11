package graphql

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	gqlparser "github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/config"
	gql "skyline-mcp/internal/graphql"
)

var sdlSignature = regexp.MustCompile(`(?im)\b(type|extend)\s+query\b|\b(type|extend)\s+mutation\b|\bschema\s*\{`)

type graphQLOptKey struct{}

// GetOptimizationFromContext extracts GraphQL optimization config from context
func GetOptimizationFromContext(ctx context.Context) *config.GraphQLOptimization {
	if opt, ok := ctx.Value(graphQLOptKey{}).(*config.GraphQLOptimization); ok {
		return opt
	}
	return nil
}

// SetOptimizationInContext adds GraphQL optimization config to context
func SetOptimizationInContext(ctx context.Context, opt *config.GraphQLOptimization) context.Context {
	return context.WithValue(ctx, graphQLOptKey{}, opt)
}

// LooksLikeGraphQLSDL reports whether the payload resembles GraphQL SDL.
func LooksLikeGraphQLSDL(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	return sdlSignature.Match(raw)
}

// ParseToCanonical parses GraphQL SDL or introspection JSON into a canonical Service.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	if LooksLikeGraphQLIntrospection(raw) {
		return ParseIntrospectionToCanonicalWithContext(ctx, raw, apiName, baseURLOverride)
	}
	if !LooksLikeGraphQLSDL(raw) {
		return nil, fmt.Errorf("graphql: unsupported schema payload")
	}
	schema, err := gqlparser.LoadSchema(&ast.Source{Name: apiName, Input: string(raw)})
	if err != nil {
		return nil, fmt.Errorf("graphql sdl: parse failed: %w", err)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("graphql sdl: base_url_override is required")
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	// Check if CRUD grouping optimization is enabled
	opt := GetOptimizationFromContext(ctx)
	if opt != nil && opt.EnableCRUDGrouping {
		// Use analyzer to detect patterns and generate composite tools for mutations
		analyzer := gql.NewSchemaAnalyzer(schema)
		patterns := analyzer.DetectCRUDPatterns()
		
		ops, err := generateCompositeTools(schema, apiName, baseURL, patterns)
		if err != nil {
			return nil, fmt.Errorf("graphql: generate composite tools: %w", err)
		}
		service.Operations = ops
		
		// CRITICAL FIX: Also include ALL query operations (reads are just as important!)
		if schema.Query != nil {
			if err := appendGraphQLOps(service, schema, schema.Query, "query"); err != nil {
				return nil, err
			}
		}
	} else {
		// Default behavior: 1:1 mapping of operations to tools
		if schema.Query != nil {
			if err := appendGraphQLOps(service, schema, schema.Query, "query"); err != nil {
				return nil, err
			}
		}
		if schema.Mutation != nil {
			if err := appendGraphQLOps(service, schema, schema.Mutation, "mutation"); err != nil {
				return nil, err
			}
		}
	}

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("graphql sdl: no query or mutation fields found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})
	return service, nil
}

func appendGraphQLOps(service *canonical.Service, schema *ast.Schema, def *ast.Definition, opType string) error {
	if def == nil {
		return nil
	}
	fields := make([]*ast.FieldDefinition, 0, len(def.Fields))
	for _, field := range def.Fields {
		if field != nil {
			fields = append(fields, field)
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	for _, field := range fields {
		op, err := buildOperation(schema, service.Name, opType, field)
		if err != nil {
			return err
		}
		service.Operations = append(service.Operations, op)
	}
	return nil
}

func buildOperation(schema *ast.Schema, apiName, opType string, field *ast.FieldDefinition) (*canonical.Operation, error) {
	if field == nil || strings.TrimSpace(field.Name) == "" {
		return nil, fmt.Errorf("graphql sdl: missing field name")
	}

	operationID := fmt.Sprintf("%s_%s", opType, field.Name)
	toolName := canonical.ToolName(apiName, operationID)

	properties := map[string]any{}
	required := []string{}
	params := []canonical.Parameter{}
	argTypes := map[string]string{}

	args := make([]*ast.ArgumentDefinition, 0, len(field.Arguments))
	for _, arg := range field.Arguments {
		if arg != nil {
			args = append(args, arg)
		}
	}
	sort.Slice(args, func(i, j int) bool { return args[i].Name < args[j].Name })
	for _, arg := range args {
		name := strings.TrimSpace(arg.Name)
		if name == "" {
			continue
		}
		if name == "selection" {
			return nil, fmt.Errorf("graphql sdl: argument name %q is reserved", name)
		}
		argTypes[name] = formatType(arg.Type)
		schema := inputSchemaForType(schema, arg.Type, 0)
		if desc := strings.TrimSpace(arg.Description); desc != "" {
			schema["description"] = desc
		}
		requiredArg := arg.Type != nil && arg.Type.NonNull
		params = append(params, canonical.Parameter{
			Name:     name,
			In:       "argument",
			Required: requiredArg,
			Schema:   schema,
		})
		properties[name] = schema
		if requiredArg {
			required = append(required, name)
		}
	}

	selection, requiresSelection := defaultSelection(schema, field.Type)
	if requiresSelection {
		selectionSchema := map[string]any{
			"type":        "string",
			"description": "Selection set for the GraphQL response. Defaults to a safe scalar selection when omitted.",
		}
		properties["selection"] = selectionSchema
		params = append(params, canonical.Parameter{
			Name:     "selection",
			In:       "selection",
			Required: false,
			Schema:   selectionSchema,
		})
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		inputSchema["required"] = uniqueSorted(required)
	}

	summary := strings.TrimSpace(field.Description)
	if summary == "" {
		summary = fmt.Sprintf("GraphQL %s %s", opType, field.Name)
	}

	responseSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"data": map[string]any{"type": "object"},
			"errors": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "object"},
			},
		},
	}

	return &canonical.Operation{
		ServiceName:    apiName,
		ID:             operationID,
		ToolName:       toolName,
		Method:         "post",
		Path:           "",
		Summary:        summary,
		Parameters:     params,
		RequestBody:    &canonical.RequestBody{Required: true, ContentType: "application/json", Schema: map[string]any{"type": "object"}},
		InputSchema:    inputSchema,
		ResponseSchema: responseSchema,
		StaticHeaders:  map[string]string{"Accept": "application/json"},
		GraphQL: &canonical.GraphQLOperation{
			OperationType:     opType,
			FieldName:         field.Name,
			ArgTypes:          argTypes,
			DefaultSelection:  selection,
			RequiresSelection: requiresSelection,
		},
	}, nil
}

func inputSchemaForType(schema *ast.Schema, typ *ast.Type, depth int) map[string]any {
	if typ == nil || depth > 8 {
		return map[string]any{"type": "object"}
	}
	if typ.Elem != nil {
		return map[string]any{
			"type":  "array",
			"items": inputSchemaForType(schema, typ.Elem, depth+1),
		}
	}
	name := strings.TrimSpace(typ.NamedType)
	if name == "" {
		return map[string]any{"type": "object"}
	}
	if isBuiltinScalar(name) {
		return map[string]any{"type": scalarType(name)}
	}
	if def := schema.Types[name]; def != nil {
		switch def.Kind {
		case ast.Enum:
			return map[string]any{"type": "string"}
		case ast.InputObject:
			return map[string]any{"type": "object", "additionalProperties": true}
		case ast.Scalar:
			return map[string]any{"type": scalarType(name)}
		default:
			return map[string]any{"type": "object"}
		}
	}
	return map[string]any{"type": "object"}
}

func defaultSelection(schema *ast.Schema, typ *ast.Type) (string, bool) {
	if isLeafType(schema, typ) {
		return "", false
	}
	name := baseTypeName(typ)
	if name == "" {
		return "{ __typename }", true
	}
	def := schema.Types[name]
	if def == nil {
		return "{ __typename }", true
	}
	switch def.Kind {
	case ast.Object:
		fields := scalarFieldNames(schema, def)
		if len(fields) == 0 {
			return "{ __typename }", true
		}
		return "{ " + strings.Join(fields, " ") + " }", true
	case ast.Interface, ast.Union:
		return "{ __typename }", true
	default:
		return "", false
	}
}

func scalarFieldNames(schema *ast.Schema, def *ast.Definition) []string {
	if def == nil {
		return nil
	}
	fields := make([]string, 0, len(def.Fields))
	for _, field := range def.Fields {
		if field == nil || field.Name == "" {
			continue
		}
		if fieldRequiresArgs(field) {
			continue
		}
		if isLeafType(schema, field.Type) {
			fields = append(fields, field.Name)
		}
	}
	sort.Strings(fields)
	return fields
}

func fieldRequiresArgs(field *ast.FieldDefinition) bool {
	for _, arg := range field.Arguments {
		if arg != nil && arg.Type != nil && arg.Type.NonNull {
			return true
		}
	}
	return false
}

func isLeafType(schema *ast.Schema, typ *ast.Type) bool {
	name := baseTypeName(typ)
	if name == "" {
		return false
	}
	if isBuiltinScalar(name) {
		return true
	}
	def := schema.Types[name]
	if def == nil {
		return false
	}
	return def.Kind == ast.Scalar || def.Kind == ast.Enum
}

func baseTypeName(typ *ast.Type) string {
	if typ == nil {
		return ""
	}
	if typ.Elem != nil {
		return baseTypeName(typ.Elem)
	}
	return typ.NamedType
}

func formatType(typ *ast.Type) string {
	if typ == nil {
		return "String"
	}
	if typ.Elem != nil {
		inner := formatType(typ.Elem)
		if typ.NonNull {
			return "[" + inner + "]!"
		}
		return "[" + inner + "]"
	}
	if typ.NonNull {
		return typ.NamedType + "!"
	}
	return typ.NamedType
}

func isBuiltinScalar(name string) bool {
	switch name {
	case "String", "Int", "Float", "Boolean", "ID":
		return true
	default:
		return false
	}
}

func scalarType(name string) string {
	switch name {
	case "Int":
		return "integer"
	case "Float":
		return "number"
	case "Boolean":
		return "boolean"
	case "String", "ID":
		return "string"
	default:
		return "string"
	}
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
