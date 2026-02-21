package swagger2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi2"
	"github.com/getkin/kin-openapi/openapi2conv"
	"gopkg.in/yaml.v3"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/parsers/openapi"
)

func LooksLikeSwagger2(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	return strings.Contains(lower, "\"swagger\"") || strings.Contains(lower, "swagger:")
}

func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	// Pre-process: normalize array-based "items" fields to single schemas.
	// Some specs (e.g. Slack) use "items": [{schema}, {"type":"null"}] which is
	// valid JSON Schema draft-04 tuple syntax but kin-openapi rejects it.
	raw = normalizeArrayItems(raw)

	var doc2 openapi2.T
	if err := json.Unmarshal(raw, &doc2); err != nil {
		if err := yaml.Unmarshal(raw, &doc2); err != nil {
			return nil, fmt.Errorf("swagger2: decode failed: %w", err)
		}
	}
	if doc2.Swagger == "" {
		return nil, fmt.Errorf("swagger2: missing swagger version")
	}
	v3, err := openapi2conv.ToV3(&doc2)
	if err != nil {
		return nil, fmt.Errorf("swagger2: convert to v3 failed: %w", err)
	}
	data, err := json.Marshal(v3)
	if err != nil {
		return nil, fmt.Errorf("swagger2: encode v3 failed: %w", err)
	}
	// The v3 conversion can re-introduce "type":"null" from $ref resolution.
	// Normalize the v3 output as well.
	data = normalizeArrayItems(data)
	return openapi.ParseToCanonical(ctx, data, apiName, baseURLOverride)
}

// normalizeArrayItems walks a JSON tree and converts any "items": [array]
// into "items": {single schema}, picking the first non-null entry.
func normalizeArrayItems(raw []byte) []byte {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw
	}
	fixArrayItems(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return out
}

func fixArrayItems(v any) {
	switch obj := v.(type) {
	case map[string]any:
		// Fix "items": [schema1, schema2, ...] → "items": schema1
		if items, ok := obj["items"]; ok {
			if arr, isArr := items.([]any); isArr && len(arr) > 0 {
				picked := pickNonNull(arr)
				// If this object is ONLY {"items": [...]}, it's a union type
				// definition — replace with the first schema's contents directly.
				if isItemsOnly(obj) {
					if m, ok := picked.(map[string]any); ok {
						delete(obj, "items")
						for k, v := range m {
							obj[k] = v
						}
					} else {
						obj["items"] = picked
					}
				} else {
					obj["items"] = picked
				}
			}
		}
		// Fix "type": ["null", "string"] → "type": "string"
		if t, ok := obj["type"]; ok {
			switch tt := t.(type) {
			case []any:
				if len(tt) > 0 {
					obj["type"] = pickNonNullType(tt)
				}
			case string:
				// Replace standalone "type":"null" with "type":"object"
				if tt == "null" {
					obj["type"] = "object"
				}
			}
		}
		for _, val := range obj {
			fixArrayItems(val)
		}
	case []any:
		for _, item := range obj {
			fixArrayItems(item)
		}
	}
}

// isItemsOnly checks if a schema object contains only "items" (and optionally
// "title" or "description") but no "type", "properties", or "$ref".
func isItemsOnly(obj map[string]any) bool {
	for key := range obj {
		switch key {
		case "items", "title", "description":
			// metadata-only keys
		default:
			return false
		}
	}
	return true
}

func pickNonNull(schemas []any) any {
	for _, s := range schemas {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if t, hasType := m["type"]; hasType {
			if ts, isStr := t.(string); isStr && ts == "null" {
				continue
			}
		}
		return s
	}
	return schemas[0]
}

// pickNonNullType picks the first non-"null" type from a type array like ["null", "string"].
func pickNonNullType(types []any) string {
	for _, t := range types {
		if s, ok := t.(string); ok && s != "null" {
			return s
		}
	}
	if len(types) > 0 {
		if s, ok := types[0].(string); ok {
			return s
		}
	}
	return "string"
}
