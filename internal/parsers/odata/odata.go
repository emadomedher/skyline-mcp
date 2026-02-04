package odata

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"mcp-api-bridge/internal/canonical"
)

// LooksLikeODataMetadata reports whether raw looks like an OData CSDL $metadata document.
func LooksLikeODataMetadata(raw []byte) bool {
	s := string(raw)
	return strings.Contains(s, "edmx:Edmx") ||
		strings.Contains(s, "<edmx:DataServices") ||
		strings.Contains(s, "oasis-open.org/odata")
}

// ParseToCanonical parses an OData CSDL $metadata XML document into a canonical Service.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx

	edmx, err := parseCSDL(raw)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("odata: base_url_override is required (OData $metadata does not contain a base URL)")
	}

	// Build entity type map across all schemas.
	typeMap := map[string]EntityType{}
	for _, schema := range edmx.DataServices.Schemas {
		for _, et := range schema.EntityTypes {
			qualified := schema.Namespace + "." + et.Name
			typeMap[qualified] = et
			typeMap[et.Name] = et // also store unqualified for convenience
		}
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	for _, schema := range edmx.DataServices.Schemas {
		for _, container := range schema.EntityContainers {
			for _, es := range container.EntitySets {
				et, ok := typeMap[es.EntityType]
				if !ok {
					continue
				}
				ops := buildEntitySetOperations(apiName, es.Name, et)
				service.Operations = append(service.Operations, ops...)
			}
		}
	}

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("odata: no entity sets found in metadata")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

func buildEntitySetOperations(apiName, setName string, et EntityType) []*canonical.Operation {
	keyName := ""
	if len(et.Key.PropertyRefs) > 0 {
		keyName = et.Key.PropertyRefs[0].Name
	}

	properties := map[string]any{}
	required := []string{}
	for _, prop := range et.Properties {
		properties[prop.Name] = edmTypeToJSONSchema(prop.Type, prop.Nullable)
		if !prop.Nullable && !isKeyProperty(prop.Name, et.Key) {
			required = append(required, prop.Name)
		}
	}

	bodySchema := map[string]any{
		"type":                 "object",
		"properties":          properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		sort.Strings(required)
		bodySchema["required"] = required
	}

	queryDesc := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"$filter":  map[string]any{"type": "string", "description": "OData filter expression (e.g. Year gt 2000)"},
			"$top":     map[string]any{"type": "integer", "description": "Maximum number of results to return"},
			"$skip":    map[string]any{"type": "integer", "description": "Number of results to skip"},
			"$orderby": map[string]any{"type": "string", "description": "Sort expression (e.g. Rating desc)"},
			"$select":  map[string]any{"type": "string", "description": "Comma-separated list of properties to return"},
			"$count":   map[string]any{"type": "string", "description": "Set to 'true' to include total count"},
		},
		"additionalProperties": false,
	}

	var ops []*canonical.Operation

	// List
	listID := "list" + setName
	listInputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"queryOptions": queryDesc,
		},
		"additionalProperties": false,
	}
	ops = append(ops, &canonical.Operation{
		ServiceName:       apiName,
		ID:                listID,
		ToolName:          canonical.ToolName(apiName, listID),
		Method:            "get",
		Path:              "/" + setName,
		Summary:           fmt.Sprintf("List %s. Supports OData query options: $filter, $top, $skip, $orderby, $select, $count.", setName),
		InputSchema:       listInputSchema,
		QueryParamsObject: "queryOptions",
	})

	if keyName != "" {
		keySchema := edmTypeToJSONSchema(keyPropertyType(keyName, et), false)

		// Get by key
		getID := "get" + setName
		getInputSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				keyName: keySchema,
			},
			"required":             []string{keyName},
			"additionalProperties": false,
		}
		ops = append(ops, &canonical.Operation{
			ServiceName: apiName,
			ID:          getID,
			ToolName:    canonical.ToolName(apiName, getID),
			Method:      "get",
			Path:        fmt.Sprintf("/%s({%s})", setName, keyName),
			Summary:     fmt.Sprintf("Get a single %s by %s.", setName, keyName),
			Parameters:  []canonical.Parameter{{Name: keyName, In: "path", Required: true, Schema: keySchema}},
			InputSchema: getInputSchema,
		})

		// Create
		createID := "create" + setName
		createInputSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"body": bodySchema,
			},
			"required":             []string{"body"},
			"additionalProperties": false,
		}
		ops = append(ops, &canonical.Operation{
			ServiceName: apiName,
			ID:          createID,
			ToolName:    canonical.ToolName(apiName, createID),
			Method:      "post",
			Path:        "/" + setName,
			Summary:     fmt.Sprintf("Create a new %s.", setName),
			RequestBody: &canonical.RequestBody{Required: true, ContentType: "application/json", Schema: bodySchema},
			InputSchema: createInputSchema,
		})

		// Update (PATCH)
		updateID := "update" + setName
		updateInputSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				keyName: keySchema,
				"body":  bodySchema,
			},
			"required":             []string{keyName, "body"},
			"additionalProperties": false,
		}
		ops = append(ops, &canonical.Operation{
			ServiceName: apiName,
			ID:          updateID,
			ToolName:    canonical.ToolName(apiName, updateID),
			Method:      "patch",
			Path:        fmt.Sprintf("/%s({%s})", setName, keyName),
			Summary:     fmt.Sprintf("Update a %s by %s (partial update).", setName, keyName),
			Parameters:  []canonical.Parameter{{Name: keyName, In: "path", Required: true, Schema: keySchema}},
			RequestBody: &canonical.RequestBody{Required: true, ContentType: "application/json", Schema: bodySchema},
			InputSchema: updateInputSchema,
		})

		// Delete
		deleteID := "delete" + setName
		deleteInputSchema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				keyName: keySchema,
			},
			"required":             []string{keyName},
			"additionalProperties": false,
		}
		ops = append(ops, &canonical.Operation{
			ServiceName: apiName,
			ID:          deleteID,
			ToolName:    canonical.ToolName(apiName, deleteID),
			Method:      "delete",
			Path:        fmt.Sprintf("/%s({%s})", setName, keyName),
			Summary:     fmt.Sprintf("Delete a %s by %s.", setName, keyName),
			Parameters:  []canonical.Parameter{{Name: keyName, In: "path", Required: true, Schema: keySchema}},
			InputSchema: deleteInputSchema,
		})
	}

	return ops
}

func edmTypeToJSONSchema(edmType string, nullable bool) map[string]any {
	schema := map[string]any{}
	switch edmType {
	case "Edm.String":
		schema["type"] = "string"
	case "Edm.Int16", "Edm.Int32", "Edm.Int64", "Edm.Byte", "Edm.SByte":
		schema["type"] = "integer"
	case "Edm.Double", "Edm.Single", "Edm.Decimal":
		schema["type"] = "number"
	case "Edm.Boolean":
		schema["type"] = "boolean"
	case "Edm.DateTimeOffset":
		schema["type"] = "string"
		schema["format"] = "date-time"
	case "Edm.Date":
		schema["type"] = "string"
		schema["format"] = "date"
	case "Edm.TimeOfDay":
		schema["type"] = "string"
		schema["format"] = "time"
	case "Edm.Guid":
		schema["type"] = "string"
		schema["format"] = "uuid"
	case "Edm.Binary":
		schema["type"] = "string"
		schema["format"] = "byte"
	default:
		schema["type"] = "string"
	}
	return schema
}

func isKeyProperty(name string, key Key) bool {
	for _, ref := range key.PropertyRefs {
		if ref.Name == name {
			return true
		}
	}
	return false
}

func keyPropertyType(keyName string, et EntityType) string {
	for _, prop := range et.Properties {
		if prop.Name == keyName {
			return prop.Type
		}
	}
	return "Edm.String"
}

// CSDL XML structures

type Edmx struct {
	XMLName      xml.Name     `xml:"Edmx"`
	DataServices DataServices `xml:"DataServices"`
}

type DataServices struct {
	Schemas []Schema `xml:"Schema"`
}

type Schema struct {
	Namespace        string            `xml:"Namespace,attr"`
	EntityTypes      []EntityType      `xml:"EntityType"`
	EntityContainers []EntityContainer `xml:"EntityContainer"`
}

type EntityType struct {
	Name       string     `xml:"Name,attr"`
	Key        Key        `xml:"Key"`
	Properties []Property `xml:"Property"`
}

type Key struct {
	PropertyRefs []PropertyRef `xml:"PropertyRef"`
}

type PropertyRef struct {
	Name string `xml:"Name,attr"`
}

type Property struct {
	Name     string `xml:"Name,attr"`
	Type     string `xml:"Type,attr"`
	Nullable bool   `xml:"Nullable,attr"`
}

type EntityContainer struct {
	Name       string      `xml:"Name,attr"`
	EntitySets []EntitySet `xml:"EntitySet"`
}

type EntitySet struct {
	Name       string `xml:"Name,attr"`
	EntityType string `xml:"EntityType,attr"`
}

func parseCSDL(raw []byte) (*Edmx, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = false
	var edmx Edmx
	if err := decoder.Decode(&edmx); err != nil {
		return nil, fmt.Errorf("odata: decode CSDL failed: %w", err)
	}
	return &edmx, nil
}
