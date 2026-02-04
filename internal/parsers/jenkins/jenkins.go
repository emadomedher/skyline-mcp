package jenkins

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"

	"mcp-api-bridge/internal/canonical"
)

// LooksLikeJenkins reports whether the payload matches Jenkins API responses.
func LooksLikeJenkins(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '{' {
		var payload map[string]any
		if err := json.Unmarshal(trimmed, &payload); err == nil {
			if cls, ok := payload["_class"].(string); ok {
				return isJenkinsClass(cls)
			}
		}
	}
	lower := strings.ToLower(string(trimmed))
	return strings.Contains(lower, "<hudson") || strings.Contains(lower, "<jenkins")
}

// ParseToCanonical returns a read-only Jenkins service model.
func ParseToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	_ = ctx
	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		if url := extractURLFromJSON(raw); url != "" {
			baseURL = strings.TrimRight(url, "/")
		} else if url := extractURLFromXML(raw); url != "" {
			baseURL = strings.TrimRight(url, "/")
		}
	}
	if baseURL == "" {
		return nil, fmt.Errorf("jenkins: base URL missing; set base_url_override or use /api/json with url field")
	}

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	queryParams := []canonical.Parameter{
		{
			Name:     "tree",
			In:       "query",
			Required: false,
			Schema: map[string]any{
				"type":        "string",
				"description": "Jenkins tree query to limit payload. Example to list jobs: jobs[name,url,color].",
			},
		},
		{
			Name:     "depth",
			In:       "query",
			Required: false,
			Schema: map[string]any{
				"type":        "integer",
				"description": "Depth of traversal for Jenkins API.",
			},
		},
	}

	rootSchema := map[string]any{
		"type": "object",
		"description": "Jenkins root object. Use tree to list jobs, e.g. jobs[name,url,color].",
		"properties": map[string]any{
			"tree": map[string]any{
				"type":        "string",
				"description": "Jenkins tree query to limit payload. Example to list jobs: jobs[name,url,color].",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": "Depth of traversal for Jenkins API.",
			},
		},
		"additionalProperties": false,
	}

	objectSchema := map[string]any{
		"type": "object",
		"description": "Jenkins object lookup. Provide a URL or path and optional tree/depth.",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Jenkins object URL or path (e.g. https://ci.example.com/job/foo/ or /job/foo/). Must be same host; /api/json appended if missing.",
			},
			"tree": map[string]any{
				"type":        "string",
				"description": "Jenkins tree query to limit payload, e.g. builds[number,url].",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": "Depth of traversal for Jenkins API.",
			},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}

	service.Operations = append(service.Operations,
		&canonical.Operation{
			ServiceName:   apiName,
			ID:            "getRoot",
			ToolName:      canonical.ToolName(apiName, "getRoot"),
			Method:        "get",
			Path:          "/api/json",
			Summary:       "Get Jenkins root object. Use tree=jobs[name,url,color] to list jobs.",
			Parameters:    queryParams,
			InputSchema:   rootSchema,
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		&canonical.Operation{
			ServiceName:    apiName,
			ID:             "getObject",
			ToolName:       canonical.ToolName(apiName, "getObject"),
			Method:         "get",
			Path:           "/api/json",
			Summary:        "Get a Jenkins object by URL/path (same host). Use tree to limit payload.",
			Parameters:     queryParams,
			InputSchema:    objectSchema,
			StaticHeaders:  map[string]string{"Accept": "application/json"},
			DynamicURLParam: "url",
		},
	)

	return service, nil
}

func isJenkinsClass(className string) bool {
	lower := strings.ToLower(className)
	return strings.HasPrefix(lower, "hudson.") || strings.HasPrefix(lower, "jenkins.") || strings.HasPrefix(lower, "org.jenkinsci.")
}

func extractURLFromJSON(raw []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if urlVal, ok := payload["url"].(string); ok {
		return strings.TrimSpace(urlVal)
	}
	return ""
}

func extractURLFromXML(raw []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := decoder.Token()
		if err != nil {
			return ""
		}
		if start, ok := tok.(xml.StartElement); ok {
			if strings.EqualFold(start.Name.Local, "url") {
				var value string
				if err := decoder.DecodeElement(&value, &start); err == nil {
					return strings.TrimSpace(value)
				}
			}
		}
	}
}
