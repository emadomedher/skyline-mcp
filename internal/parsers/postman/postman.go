package postman

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"mcp-api-bridge/internal/canonical"
)

// LooksLikePostmanCollection reports whether raw looks like a Postman Collection v2.x JSON.
func LooksLikePostmanCollection(raw []byte) bool {
	var doc struct {
		Info struct {
			Schema string `json:"schema"`
		} `json:"info"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(doc.Info.Schema), "schema.getpostman.com")
}

// ParseToCanonical parses a Postman Collection v2.1 JSON into a canonical Service.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx

	var col Collection
	if err := json.Unmarshal(raw, &col); err != nil {
		return nil, fmt.Errorf("postman: decode failed: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		// Try to extract from collection variables.
		for _, v := range col.Variable {
			if v.Key == "baseUrl" || v.Key == "base_url" || v.Key == "BASE_URL" {
				baseURL = strings.TrimRight(v.Value, "/")
				break
			}
		}
	}
	if baseURL == "" {
		return nil, fmt.Errorf("postman: base_url_override is required (or set a baseUrl collection variable)")
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	walkItems(service, apiName, col.Item, "")

	if len(service.Operations) == 0 {
		return nil, fmt.Errorf("postman: no request items found")
	}

	sort.Slice(service.Operations, func(i, j int) bool {
		return service.Operations[i].ToolName < service.Operations[j].ToolName
	})

	return service, nil
}

var postmanVarRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

func walkItems(service *canonical.Service, apiName string, items []Item, prefix string) {
	for _, item := range items {
		if len(item.Item) > 0 {
			// Folder: recurse with name prefix.
			folderPrefix := prefix
			if item.Name != "" {
				if folderPrefix != "" {
					folderPrefix += "_"
				}
				folderPrefix += sanitizeName(item.Name)
			}
			walkItems(service, apiName, item.Item, folderPrefix)
			continue
		}
		if item.Request == nil {
			continue
		}

		op := buildOperation(apiName, item, prefix)
		if op != nil {
			service.Operations = append(service.Operations, op)
		}
	}
}

func buildOperation(apiName string, item Item, prefix string) *canonical.Operation {
	req := item.Request

	method := strings.ToLower(req.Method)
	if method == "" {
		method = "get"
	}

	rawPath, pathVars, queryParams := parseURL(req.URL)

	operationID := prefix
	if operationID != "" {
		operationID += "_"
	}
	operationID += sanitizeName(item.Name)
	if operationID == "" {
		operationID = method + "_op"
	}

	toolName := canonical.ToolName(apiName, operationID)

	summary := item.Name
	if req.Description != "" {
		summary = item.Name + ": " + req.Description
	}

	var params []canonical.Parameter
	for _, pv := range pathVars {
		params = append(params, canonical.Parameter{
			Name:     pv,
			In:       "path",
			Required: true,
			Schema:   map[string]any{"type": "string"},
		})
	}
	for _, qp := range queryParams {
		params = append(params, canonical.Parameter{
			Name:     qp.Key,
			In:       "query",
			Required: false,
			Schema:   map[string]any{"type": "string", "description": qp.Description},
		})
	}
	for _, h := range req.Header {
		if h.Disabled {
			continue
		}
		lower := strings.ToLower(h.Key)
		if lower == "content-type" || lower == "authorization" || lower == "accept" {
			continue
		}
		params = append(params, canonical.Parameter{
			Name:     h.Key,
			In:       "header",
			Required: false,
			Schema:   map[string]any{"type": "string"},
		})
	}

	properties := map[string]any{}
	requiredFields := []string{}
	for _, p := range params {
		properties[p.Name] = p.Schema
		if p.Required {
			requiredFields = append(requiredFields, p.Name)
		}
	}

	var reqBody *canonical.RequestBody
	if req.Body != nil && req.Body.Mode != "" {
		ct := "application/json"
		switch req.Body.Mode {
		case "formdata":
			ct = "multipart/form-data"
		case "urlencoded":
			ct = "application/x-www-form-urlencoded"
		case "raw":
			if strings.Contains(strings.ToLower(req.Body.Options.Raw.Language), "xml") {
				ct = "application/xml"
			}
		}
		reqBody = &canonical.RequestBody{
			Required:    true,
			ContentType: ct,
			Schema:      map[string]any{"type": "object", "additionalProperties": true},
		}
		properties["body"] = map[string]any{"type": "object", "additionalProperties": true, "description": "Request body"}
		requiredFields = append(requiredFields, "body")
	}

	inputSchema := map[string]any{
		"type":                 "object",
		"properties":          properties,
		"additionalProperties": false,
	}
	if len(requiredFields) > 0 {
		sort.Strings(requiredFields)
		inputSchema["required"] = requiredFields
	}

	return &canonical.Operation{
		ServiceName: apiName,
		ID:          operationID,
		ToolName:    toolName,
		Method:      method,
		Path:        rawPath,
		Summary:     summary,
		Parameters:  params,
		RequestBody: reqBody,
		InputSchema: inputSchema,
	}
}

func parseURL(u any) (path string, pathVars []string, queryParams []QueryParam) {
	switch v := u.(type) {
	case string:
		// Simple string URL: replace {{var}} with {var} and extract path.
		path = postmanVarRe.ReplaceAllString(v, "{$1}")
		// Strip scheme + host to get just the path.
		if idx := strings.Index(path, "://"); idx != -1 {
			rest := path[idx+3:]
			if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
				path = rest[slashIdx:]
			} else {
				path = "/"
			}
		}
		// Extract path vars.
		for _, m := range postmanVarRe.FindAllStringSubmatch(fmt.Sprintf("%v", u), -1) {
			pathVars = append(pathVars, m[1])
		}
		return
	case map[string]any:
		return parseURLObject(v)
	default:
		// Try marshaling back and parsing as URLObject.
		raw, err := json.Marshal(u)
		if err != nil {
			return "/", nil, nil
		}
		var urlObj URLObject
		if err := json.Unmarshal(raw, &urlObj); err != nil {
			return "/", nil, nil
		}
		return parseURLObjectStruct(urlObj)
	}
}

func parseURLObject(m map[string]any) (string, []string, []QueryParam) {
	raw, _ := json.Marshal(m)
	var urlObj URLObject
	if err := json.Unmarshal(raw, &urlObj); err != nil {
		return "/", nil, nil
	}
	return parseURLObjectStruct(urlObj)
}

func parseURLObjectStruct(urlObj URLObject) (string, []string, []QueryParam) {
	// Build path from path segments.
	var pathParts []string
	var pathVars []string
	for _, seg := range urlObj.Path {
		if strings.HasPrefix(seg, ":") {
			varName := strings.TrimPrefix(seg, ":")
			pathParts = append(pathParts, "{"+varName+"}")
			pathVars = append(pathVars, varName)
		} else {
			replaced := postmanVarRe.ReplaceAllString(seg, "{$1}")
			pathParts = append(pathParts, replaced)
		}
	}
	path := "/" + strings.Join(pathParts, "/")

	// Extract variables from URL variable[] (path params).
	for _, v := range urlObj.Variable {
		found := false
		for _, pv := range pathVars {
			if pv == v.Key {
				found = true
				break
			}
		}
		if !found {
			pathVars = append(pathVars, v.Key)
		}
	}

	return path, pathVars, urlObj.Query
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == ' ' || r == '-' || r == '/':
			b.WriteRune('_')
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Postman Collection v2.1 JSON structures.

type Collection struct {
	Info     Info       `json:"info"`
	Item     []Item     `json:"item"`
	Variable []Variable `json:"variable"`
}

type Info struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

type Item struct {
	Name    string   `json:"name"`
	Request *Request `json:"request,omitempty"`
	Item    []Item   `json:"item,omitempty"` // folder children
}

type Request struct {
	Method      string   `json:"method"`
	URL         any      `json:"url"` // string or URLObject
	Header      []Header `json:"header"`
	Body        *Body    `json:"body,omitempty"`
	Description string   `json:"description"`
}

type URLObject struct {
	Raw      string       `json:"raw"`
	Host     []string     `json:"host"`
	Path     []string     `json:"path"`
	Query    []QueryParam `json:"query"`
	Variable []Variable   `json:"variable"`
}

type QueryParam struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Disabled    bool   `json:"disabled"`
}

type Header struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

type Body struct {
	Mode    string      `json:"mode"`
	Raw     string      `json:"raw"`
	Options BodyOptions `json:"options"`
}

type BodyOptions struct {
	Raw RawOptions `json:"raw"`
}

type RawOptions struct {
	Language string `json:"language"`
}

type Variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
