package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"skyline-mcp/internal/canonical"
	gql "skyline-mcp/internal/graphql"
)

type introspectionResponse struct {
	Data   introspectionData `json:"data"`
	Errors []any             `json:"errors"`
}

type introspectionData struct {
	Schema introspectionSchema `json:"__schema"`
}

type introspectionSchema struct {
	QueryType    *introspectionTypeRef `json:"queryType"`
	MutationType *introspectionTypeRef `json:"mutationType"`
	Types        []introspectionType   `json:"types"`
}

type introspectionTypeRef struct {
	Kind   string                `json:"kind"`
	Name   string                `json:"name"`
	OfType *introspectionTypeRef `json:"ofType"`
}

type introspectionType struct {
	Kind        string                   `json:"kind"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Fields      []introspectionField     `json:"fields"`
	InputFields []introspectionInput     `json:"inputFields"`
	EnumValues  []introspectionEnumValue `json:"enumValues"`
}

type introspectionField struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Args        []introspectionInput `json:"args"`
	Type        introspectionTypeRef `json:"type"`
}

type introspectionInput struct {
	Name         string               `json:"name"`
	Description  string               `json:"description"`
	DefaultValue *string              `json:"defaultValue"`
	Type         introspectionTypeRef `json:"type"`
}

type introspectionEnumValue struct {
	Name string `json:"name"`
}

func LooksLikeGraphQLIntrospection(raw []byte) bool {
	var payload introspectionResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	return payload.Data.Schema.Types != nil
}

func ParseIntrospectionToCanonical(raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	var payload introspectionResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("graphql introspection: parse failed: %w", err)
	}
	if payload.Data.Schema.Types == nil {
		return nil, fmt.Errorf("graphql introspection: missing schema")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("graphql introspection: base_url_override is required")
	}

	typeMap := map[string]introspectionType{}
	for _, t := range payload.Data.Schema.Types {
		if t.Name != "" {
			typeMap[t.Name] = t
		}
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	if payload.Data.Schema.QueryType != nil && payload.Data.Schema.QueryType.Name != "" {
		if err := appendIntrospectionOps(service, typeMap, payload.Data.Schema.QueryType.Name, "query"); err != nil {
			return nil, err
		}
	}
	if payload.Data.Schema.MutationType != nil && payload.Data.Schema.MutationType.Name != "" {
		if err := appendIntrospectionOps(service, typeMap, payload.Data.Schema.MutationType.Name, "mutation"); err != nil {
			return nil, err
		}
	}

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("graphql introspection: no query or mutation fields found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})
	return service, nil
}

// ParseIntrospectionToCanonicalWithContext supports optimization via context
func ParseIntrospectionToCanonicalWithContext(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	var payload introspectionResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("graphql introspection: parse failed: %w", err)
	}
	if payload.Data.Schema.Types == nil {
		return nil, fmt.Errorf("graphql introspection: missing schema")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("graphql introspection: base_url_override is required")
	}

	// Check if CRUD grouping optimization is enabled
	opt := GetOptimizationFromContext(ctx)
	if opt != nil && opt.EnableCRUDGrouping {
		// Build type map (needed for query operations)
		typeMap := map[string]introspectionType{}
		for _, t := range payload.Data.Schema.Types {
			if t.Name != "" {
				typeMap[t.Name] = t
			}
		}

		// Convert introspection to AST schema for analyzer
		astSchema, err := introspectionToASTSchema(&payload.Data.Schema)
		if err != nil {
			return nil, fmt.Errorf("graphql introspection: convert to AST: %w", err)
		}

		// Use analyzer to detect patterns and generate composite tools for mutations
		analyzer := gql.NewSchemaAnalyzer(astSchema)
		patterns := analyzer.DetectCRUDPatterns()
		
		ops, err := generateCompositeTools(astSchema, apiName, baseURL, patterns)
		if err != nil {
			return nil, fmt.Errorf("graphql introspection: generate composite tools: %w", err)
		}

		service := &canonical.Service{
			Name:       apiName,
			BaseURL:    baseURL,
			Operations: ops,
		}

		// CRITICAL FIX: Also include ALL query operations (reads are just as important!)
		if payload.Data.Schema.QueryType != nil && payload.Data.Schema.QueryType.Name != "" {
			if err := appendIntrospectionOps(service, typeMap, payload.Data.Schema.QueryType.Name, "query"); err != nil {
				return nil, err
			}
		}

		return service, nil
	}

	// Default behavior: 1:1 mapping
	return ParseIntrospectionToCanonical(raw, apiName, baseURLOverride)
}

func appendIntrospectionOps(service *canonical.Service, typeMap map[string]introspectionType, typeName, opType string) error {
	root, ok := typeMap[typeName]
	if !ok {
		return fmt.Errorf("graphql introspection: root type %s not found", typeName)
	}
	fields := make([]introspectionField, 0, len(root.Fields))
	for _, f := range root.Fields {
		if f.Name != "" {
			fields = append(fields, f)
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	for _, field := range fields {
		op, err := buildIntrospectionOperation(typeMap, service.Name, opType, field)
		if err != nil {
			return err
		}
		service.Operations = append(service.Operations, op)
	}
	return nil
}

func buildIntrospectionOperation(typeMap map[string]introspectionType, apiName, opType string, field introspectionField) (*canonical.Operation, error) {
	if strings.TrimSpace(field.Name) == "" {
		return nil, fmt.Errorf("graphql introspection: missing field name")
	}
	operationID := fmt.Sprintf("%s_%s", opType, field.Name)
	toolName := canonical.ToolName(apiName, operationID)

	properties := map[string]any{}
	required := []string{}
	params := []canonical.Parameter{}
	argTypes := map[string]string{}

	args := make([]introspectionInput, 0, len(field.Args))
	for _, a := range field.Args {
		if a.Name != "" {
			args = append(args, a)
		}
	}
	sort.Slice(args, func(i, j int) bool { return args[i].Name < args[j].Name })
	for _, arg := range args {
		name := strings.TrimSpace(arg.Name)
		if name == "" {
			continue
		}
		if name == "selection" {
			return nil, fmt.Errorf("graphql introspection: argument name %q is reserved", name)
		}
		argTypes[name] = formatIntrospectionType(arg.Type)
		schema := inputSchemaForIntrospectionType(typeMap, arg.Type, 0)
		if desc := strings.TrimSpace(arg.Description); desc != "" {
			schema["description"] = desc
		}
		requiredArg := isNonNullType(arg.Type)
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

	selection, requiresSelection := defaultSelectionForIntrospection(typeMap, field.Type)
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

func inputSchemaForIntrospectionType(typeMap map[string]introspectionType, typ introspectionTypeRef, depth int) map[string]any {
	if depth > 8 {
		return map[string]any{"type": "object"}
	}
	switch typ.Kind {
	case "NON_NULL":
		if typ.OfType != nil {
			return inputSchemaForIntrospectionType(typeMap, *typ.OfType, depth+1)
		}
		return map[string]any{"type": "object"}
	case "LIST":
		if typ.OfType != nil {
			return map[string]any{
				"type":  "array",
				"items": inputSchemaForIntrospectionType(typeMap, *typ.OfType, depth+1),
			}
		}
		return map[string]any{"type": "array", "items": map[string]any{"type": "object"}}
	}

	name := typ.Name
	if name == "" {
		return map[string]any{"type": "object"}
	}
	if isBuiltinScalar(name) {
		return map[string]any{"type": scalarType(name)}
	}
	if t, ok := typeMap[name]; ok {
		switch t.Kind {
		case "ENUM":
			enumVals := make([]string, 0, len(t.EnumValues))
			for _, ev := range t.EnumValues {
				if ev.Name != "" {
					enumVals = append(enumVals, ev.Name)
				}
			}
			out := map[string]any{"type": "string"}
			if len(enumVals) > 0 {
				out["enum"] = enumVals
			}
			return out
		case "INPUT_OBJECT":
			return map[string]any{"type": "object", "additionalProperties": true}
		case "SCALAR":
			return map[string]any{"type": scalarType(name)}
		default:
			return map[string]any{"type": "object"}
		}
	}
	return map[string]any{"type": "object"}
}

func defaultSelectionForIntrospection(typeMap map[string]introspectionType, typ introspectionTypeRef) (string, bool) {
	if isLeafIntrospectionType(typeMap, typ) {
		return "", false
	}
	name := baseIntrospectionTypeName(typ)
	if name == "" {
		return "{ __typename }", true
	}
	t, ok := typeMap[name]
	if !ok {
		return "{ __typename }", true
	}
	switch t.Kind {
	case "OBJECT":
		fields := scalarFieldNamesFromIntrospection(typeMap, t)
		if len(fields) == 0 {
			return "{ __typename }", true
		}
		return "{ " + strings.Join(fields, " ") + " }", true
	case "INTERFACE", "UNION":
		return "{ __typename }", true
	default:
		return "", false
	}
}

func scalarFieldNamesFromIntrospection(typeMap map[string]introspectionType, t introspectionType) []string {
	fields := make([]string, 0, len(t.Fields))
	for _, field := range t.Fields {
		if field.Name == "" {
			continue
		}
		if fieldRequiresArgsIntrospection(field.Args) {
			continue
		}
		if isLeafIntrospectionType(typeMap, field.Type) {
			fields = append(fields, field.Name)
		}
	}
	sort.Strings(fields)
	return fields
}

func fieldRequiresArgsIntrospection(args []introspectionInput) bool {
	for _, arg := range args {
		if isNonNullType(arg.Type) {
			return true
		}
	}
	return false
}

func isLeafIntrospectionType(typeMap map[string]introspectionType, typ introspectionTypeRef) bool {
	name := baseIntrospectionTypeName(typ)
	if name == "" {
		return false
	}
	if isBuiltinScalar(name) {
		return true
	}
	t, ok := typeMap[name]
	if !ok {
		return false
	}
	return t.Kind == "SCALAR" || t.Kind == "ENUM"
}

func baseIntrospectionTypeName(typ introspectionTypeRef) string {
	switch typ.Kind {
	case "NON_NULL", "LIST":
		if typ.OfType != nil {
			return baseIntrospectionTypeName(*typ.OfType)
		}
		return ""
	default:
		return typ.Name
	}
}

func isNonNullType(typ introspectionTypeRef) bool {
	if typ.Kind == "NON_NULL" {
		return true
	}
	return false
}

func formatIntrospectionType(typ introspectionTypeRef) string {
	switch typ.Kind {
	case "NON_NULL":
		if typ.OfType != nil {
			return formatIntrospectionType(*typ.OfType) + "!"
		}
		return "String!"
	case "LIST":
		if typ.OfType != nil {
			return "[" + formatIntrospectionType(*typ.OfType) + "]"
		}
		return "[String]"
	default:
		if typ.Name != "" {
			return typ.Name
		}
		return "String"
	}
}

// introspectionToASTSchema converts introspection schema to AST schema for analyzer
func introspectionToASTSchema(schema *introspectionSchema) (*ast.Schema, error) {
	astSchema := &ast.Schema{
		Types:         make(map[string]*ast.Definition),
		Directives:    make(map[string]*ast.DirectiveDefinition),
		PossibleTypes: make(map[string][]*ast.Definition),
	}

	// Build type map
	for _, t := range schema.Types {
		if strings.HasPrefix(t.Name, "__") {
			continue // Skip introspection types
		}

		def := &ast.Definition{
			Kind:   ast.DefinitionKind(t.Kind),
			Name:   t.Name,
			Fields: make(ast.FieldList, 0),
		}

		// Add fields for OBJECT types
		if t.Kind == "OBJECT" {
			for _, f := range t.Fields {
				if f.Name == "" {
					continue
				}

				field := &ast.FieldDefinition{
					Name:      f.Name,
					Arguments: make(ast.ArgumentDefinitionList, 0),
					Type:      introspectionTypeRefToAST(f.Type),
				}

				// Add arguments
				for _, arg := range f.Args {
					if arg.Name == "" {
						continue
					}
					argDef := &ast.ArgumentDefinition{
						Name: arg.Name,
						Type: introspectionTypeRefToAST(arg.Type),
					}
					field.Arguments = append(field.Arguments, argDef)
				}

				def.Fields = append(def.Fields, field)
			}
		}

		astSchema.Types[t.Name] = def
	}

	// Set Query and Mutation root types
	if schema.QueryType != nil && schema.QueryType.Name != "" {
		astSchema.Query = astSchema.Types[schema.QueryType.Name]
	}
	if schema.MutationType != nil && schema.MutationType.Name != "" {
		astSchema.Mutation = astSchema.Types[schema.MutationType.Name]
	}

	return astSchema, nil
}

// introspectionTypeRefToAST converts introspection type reference to AST type
func introspectionTypeRefToAST(typeRef introspectionTypeRef) *ast.Type {
	// Handle NON_NULL wrapper
	if typeRef.Kind == "NON_NULL" && typeRef.OfType != nil {
		innerType := introspectionTypeRefToAST(*typeRef.OfType)
		innerType.NonNull = true
		return innerType
	}

	// Handle LIST wrapper
	if typeRef.Kind == "LIST" && typeRef.OfType != nil {
		return &ast.Type{
			Elem: introspectionTypeRefToAST(*typeRef.OfType),
		}
	}

	// Base type (OBJECT, SCALAR, ENUM, etc.)
	return &ast.Type{
		NamedType: typeRef.Name,
	}
}
