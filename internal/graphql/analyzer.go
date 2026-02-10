package graphql

import (
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// SchemaAnalyzer analyzes GraphQL schemas to detect patterns and relationships
type SchemaAnalyzer struct {
	schema *ast.Schema
}

// NewSchemaAnalyzer creates a new schema analyzer
func NewSchemaAnalyzer(schema *ast.Schema) *SchemaAnalyzer {
	return &SchemaAnalyzer{schema: schema}
}

// CRUDPattern represents a group of related CRUD operations on a type
type CRUDPattern struct {
	BaseType    string                   // e.g., "Issue"
	Create      *ast.FieldDefinition     // createIssue
	Update      *ast.FieldDefinition     // updateIssue
	Delete      *ast.FieldDefinition     // deleteIssue
	SetOps      []*ast.FieldDefinition   // issueSetLabels, issueSetAssignees, etc.
	QuerySingle *ast.FieldDefinition     // issue(id)
	QueryList   *ast.FieldDefinition     // issues(filter)
}

// DetectCRUDPatterns analyzes mutations and queries to find CRUD patterns
func (a *SchemaAnalyzer) DetectCRUDPatterns() []*CRUDPattern {
	patterns := make(map[string]*CRUDPattern)

	// Analyze mutations
	if a.schema.Mutation != nil {
		for _, field := range a.schema.Mutation.Fields {
			if field == nil {
				continue
			}

			baseType := a.extractBaseType(field.Name, field.Type)
			if baseType == "" {
				continue
			}

			if patterns[baseType] == nil {
				patterns[baseType] = &CRUDPattern{
					BaseType: baseType,
					SetOps:   []*ast.FieldDefinition{},
				}
			}

			pattern := patterns[baseType]

			// Classify by operation type
			nameLower := strings.ToLower(field.Name)
			if strings.HasPrefix(nameLower, "create") {
				pattern.Create = field
			} else if strings.HasPrefix(nameLower, "update") {
				pattern.Update = field
			} else if strings.HasPrefix(nameLower, "delete") || strings.HasPrefix(nameLower, "destroy") {
				pattern.Delete = field
			} else if strings.Contains(nameLower, "set") || strings.Contains(nameLower, "add") {
				// issueSetLabels, issueAddAssignees, etc.
				pattern.SetOps = append(pattern.SetOps, field)
			}
		}
	}

	// Analyze queries
	if a.schema.Query != nil {
		for _, field := range a.schema.Query.Fields {
			if field == nil {
				continue
			}

			baseType := a.extractBaseType(field.Name, field.Type)
			if baseType == "" {
				continue
			}

			if patterns[baseType] != nil {
				pattern := patterns[baseType]
				
				// Singular query: issue(id: ID!)
				if isSingularQuery(field) {
					pattern.QuerySingle = field
				} else {
					// List query: issues(filter: IssueFilter)
					pattern.QueryList = field
				}
			}
		}
	}

	// Convert map to sorted slice
	result := make([]*CRUDPattern, 0, len(patterns))
	for _, pattern := range patterns {
		// Only include patterns with at least create OR update
		if pattern.Create != nil || pattern.Update != nil {
			result = append(result, pattern)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].BaseType < result[j].BaseType
	})

	return result
}

// extractBaseType extracts the base type name from an operation name and return type
func (a *SchemaAnalyzer) extractBaseType(fieldName string, returnType *ast.Type) string {
	// First, try to get from return type
	typeName := baseTypeName(returnType)
	if typeName != "" && !isBuiltinType(typeName) {
		// Check if this is a payload type (e.g., CreateIssuePayload)
		if strings.HasSuffix(typeName, "Payload") {
			// Extract from payload: CreateIssuePayload → Issue
			base := strings.TrimSuffix(typeName, "Payload")
			base = strings.TrimPrefix(base, "Create")
			base = strings.TrimPrefix(base, "Update")
			base = strings.TrimPrefix(base, "Delete")
			if base != "" {
				return base
			}
		}
		return typeName
	}

	// Fallback: Extract from field name
	name := fieldName

	// Strip common prefixes
	name = strings.TrimPrefix(name, "create")
	name = strings.TrimPrefix(name, "update")
	name = strings.TrimPrefix(name, "delete")
	name = strings.TrimPrefix(name, "destroy")

	// Handle "xSetY" or "xAddY" pattern (issueSetLabels → Issue)
	if idx := strings.Index(name, "Set"); idx > 0 {
		return name[:idx]
	}
	if idx := strings.Index(name, "Add"); idx > 0 {
		return name[:idx]
	}

	// Capitalize first letter
	if len(name) > 0 {
		return strings.ToUpper(name[:1]) + name[1:]
	}

	return ""
}

// GetTypesByCategory groups types by their kind
func (a *SchemaAnalyzer) GetTypesByCategory() map[string][]string {
	categories := map[string][]string{
		"object":      {},
		"input":       {},
		"enum":        {},
		"scalar":      {},
		"interface":   {},
		"union":       {},
	}

	for name, typeDef := range a.schema.Types {
		if typeDef == nil || isBuiltinType(name) {
			continue
		}

		switch typeDef.Kind {
		case ast.Object:
			categories["object"] = append(categories["object"], name)
		case ast.InputObject:
			categories["input"] = append(categories["input"], name)
		case ast.Enum:
			categories["enum"] = append(categories["enum"], name)
		case ast.Scalar:
			categories["scalar"] = append(categories["scalar"], name)
		case ast.Interface:
			categories["interface"] = append(categories["interface"], name)
		case ast.Union:
			categories["union"] = append(categories["union"], name)
		}
	}

	// Sort for consistency
	for category := range categories {
		sort.Strings(categories[category])
	}

	return categories
}

// GetScalarFields returns all scalar (leaf) fields for a given type
func (a *SchemaAnalyzer) GetScalarFields(typeName string) []string {
	typeDef := a.schema.Types[typeName]
	if typeDef == nil || typeDef.Kind != ast.Object {
		return nil
	}

	fields := []string{}
	for _, field := range typeDef.Fields {
		if field == nil || field.Name == "" {
			continue
		}

		// Skip fields that require arguments
		if fieldRequiresArgs(field) {
			continue
		}

		// Check if field type is a scalar/enum (leaf type)
		if a.isLeafType(field.Type) {
			fields = append(fields, field.Name)
		}
	}

	sort.Strings(fields)
	return fields
}

// FlattenInputObject converts a nested InputObject type into flat parameters
func (a *SchemaAnalyzer) FlattenInputObject(typeName string) map[string]*ast.FieldDefinition {
	typeDef := a.schema.Types[typeName]
	if typeDef == nil || typeDef.Kind != ast.InputObject {
		return nil
	}

	flattened := make(map[string]*ast.FieldDefinition)
	for _, field := range typeDef.Fields {
		if field != nil && field.Name != "" {
			flattened[field.Name] = field
		}
	}

	return flattened
}

// isLeafType checks if a type is a scalar or enum (leaf type)
func (a *SchemaAnalyzer) isLeafType(typ *ast.Type) bool {
	name := baseTypeName(typ)
	if name == "" {
		return false
	}

	if isBuiltinType(name) {
		return true
	}

	typeDef := a.schema.Types[name]
	if typeDef == nil {
		return false
	}

	return typeDef.Kind == ast.Scalar || typeDef.Kind == ast.Enum
}

// Helper functions

func baseTypeName(typ *ast.Type) string {
	if typ == nil {
		return ""
	}
	if typ.Elem != nil {
		return baseTypeName(typ.Elem)
	}
	return typ.NamedType
}

func isBuiltinType(name string) bool {
	switch name {
	case "String", "Int", "Float", "Boolean", "ID":
		return true
	case "Query", "Mutation", "Subscription", "__Schema", "__Type", "__Field", "__InputValue", "__EnumValue", "__Directive":
		return true
	default:
		return false
	}
}

func isSingularQuery(field *ast.FieldDefinition) bool {
	// Check if query has an 'id' or similar required argument
	for _, arg := range field.Arguments {
		if arg == nil {
			continue
		}
		argName := strings.ToLower(arg.Name)
		if (argName == "id" || argName == "uid" || argName == "key") && arg.Type != nil && arg.Type.NonNull {
			return true
		}
	}
	return false
}

func fieldRequiresArgs(field *ast.FieldDefinition) bool {
	for _, arg := range field.Arguments {
		if arg != nil && arg.Type != nil && arg.Type.NonNull {
			return true
		}
	}
	return false
}
