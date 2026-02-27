// Package ckan implements a Skyline adapter for CKAN open data portals.
// CKAN (https://ckan.org) is the platform powering hundreds of government
// open data portals worldwide. It exposes a fixed action-based JSON API at
// /api/3/action/{action} that does not have an OpenAPI spec.
package ckan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"skyline-mcp/internal/canonical"
)

// LooksLikeCKAN reports whether raw looks like a CKAN API response.
// An empty/nil slice is treated as true so that explicit spec_type:ckan
// configs (where Parse is called with nil raw) are accepted.
func LooksLikeCKAN(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return true
	}
	if raw[0] != '{' {
		return false
	}
	var p struct {
		Success bool   `json:"success"`
		Help    string `json:"help"`
		Result  any    `json:"result"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return false
	}
	return p.Result != nil && strings.Contains(p.Help, "/api/3/action/")
}

// ParseToCanonical returns a canonical.Service with the 7 standard CKAN
// operations. raw is ignored — the tool set is fixed for all CKAN portals.
// baseURLOverride must be set (e.g. https://open.data.gov.sa).
func ParseToCanonical(_ context.Context, _ []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(baseURLOverride), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("ckan: base_url_override is required (e.g. https://data.gov)")
	}

	svc := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}

	svc.Operations = append(svc.Operations,
		searchDatasets(apiName),
		listDatasets(apiName),
		getDataset(apiName),
		getResource(apiName),
		queryDatastore(apiName),
		listOrganizations(apiName),
		listTags(apiName),
	)

	return svc, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func jsonBody(schema map[string]any) *canonical.RequestBody {
	return &canonical.RequestBody{
		Required:    false,
		ContentType: "application/json",
		Schema:      schema,
	}
}

func ckanHeaders() map[string]string {
	return map[string]string{"Accept": "application/json"}
}

// ── operations ───────────────────────────────────────────────────────────────

func searchDatasets(api string) *canonical.Operation {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q":               map[string]any{"type": "string", "description": "Full-text search query."},
			"fq":              map[string]any{"type": "string", "description": "Filter query (e.g. 'organization:my-org tags:health')."},
			"rows":            map[string]any{"type": "integer", "description": "Number of results to return (default 10)."},
			"start":           map[string]any{"type": "integer", "description": "Offset for pagination."},
			"sort":            map[string]any{"type": "string", "description": "Sort field and direction (e.g. 'score desc, metadata_modified desc')."},
			"include_private": map[string]any{"type": "boolean", "description": "Include private datasets (requires auth)."},
		},
		"additionalProperties": false,
	}
	return &canonical.Operation{
		ServiceName:   api,
		ID:            "searchDatasets",
		ToolName:      canonical.ToolName(api, "searchDatasets"),
		Method:        "post",
		Path:          "/api/3/action/package_search",
		Summary:       "Search datasets",
		Description:   "Full-text search across all datasets on this CKAN portal.",
		RequestBody:   jsonBody(schema),
		InputSchema:   schema,
		StaticHeaders: ckanHeaders(),
	}
}

func listDatasets(api string) *canonical.Operation {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit":  map[string]any{"type": "integer", "description": "Maximum number of dataset names to return."},
			"offset": map[string]any{"type": "integer", "description": "Offset for pagination."},
		},
		"additionalProperties": false,
	}
	return &canonical.Operation{
		ServiceName:   api,
		ID:            "listDatasets",
		ToolName:      canonical.ToolName(api, "listDatasets"),
		Method:        "post",
		Path:          "/api/3/action/package_list",
		Summary:       "List all dataset names",
		Description:   "Returns a list of the names of all datasets published on this portal.",
		RequestBody:   jsonBody(schema),
		InputSchema:   schema,
		StaticHeaders: ckanHeaders(),
	}
}

func getDataset(api string) *canonical.Operation {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "Dataset name or ID."},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
	return &canonical.Operation{
		ServiceName:   api,
		ID:            "getDataset",
		ToolName:      canonical.ToolName(api, "getDataset"),
		Method:        "post",
		Path:          "/api/3/action/package_show",
		Summary:       "Get dataset metadata",
		Description:   "Returns full metadata for a dataset including all its resources.",
		RequestBody:   jsonBody(schema),
		InputSchema:   schema,
		StaticHeaders: ckanHeaders(),
	}
}

func getResource(api string) *canonical.Operation {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "Resource ID."},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
	return &canonical.Operation{
		ServiceName:   api,
		ID:            "getResource",
		ToolName:      canonical.ToolName(api, "getResource"),
		Method:        "post",
		Path:          "/api/3/action/resource_show",
		Summary:       "Get resource metadata",
		Description:   "Returns metadata for a specific resource (file/link) within a dataset.",
		RequestBody:   jsonBody(schema),
		InputSchema:   schema,
		StaticHeaders: ckanHeaders(),
	}
}

func queryDatastore(api string) *canonical.Operation {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"resource_id": map[string]any{"type": "string", "description": "ID of the resource to query."},
			"q":           map[string]any{"type": "string", "description": "Full-text query against all fields."},
			"filters":     map[string]any{"type": "object", "description": "Column filters as key/value pairs (e.g. {\"country\": \"SA\"})."},
			"sort":        map[string]any{"type": "string", "description": "Column to sort by (e.g. 'year desc')."},
			"limit":       map[string]any{"type": "integer", "description": "Number of rows to return (default 100)."},
			"offset":      map[string]any{"type": "integer", "description": "Row offset for pagination."},
			"fields":      map[string]any{"type": "string", "description": "Comma-separated list of columns to return."},
		},
		"required":             []string{"resource_id"},
		"additionalProperties": false,
	}
	return &canonical.Operation{
		ServiceName:   api,
		ID:            "queryDatastore",
		ToolName:      canonical.ToolName(api, "queryDatastore"),
		Method:        "post",
		Path:          "/api/3/action/datastore_search",
		Summary:       "Query tabular data",
		Description:   "Execute a filtered query against a tabular resource stored in the CKAN datastore.",
		RequestBody:   jsonBody(schema),
		InputSchema:   schema,
		StaticHeaders: ckanHeaders(),
	}
}

func listOrganizations(api string) *canonical.Operation {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sort":       map[string]any{"type": "string", "description": "Sort field (e.g. 'name asc')."},
			"limit":      map[string]any{"type": "integer", "description": "Maximum number of organizations to return."},
			"offset":     map[string]any{"type": "integer", "description": "Offset for pagination."},
			"all_fields": map[string]any{"type": "boolean", "description": "Return full organization objects instead of just names."},
		},
		"additionalProperties": false,
	}
	return &canonical.Operation{
		ServiceName:   api,
		ID:            "listOrganizations",
		ToolName:      canonical.ToolName(api, "listOrganizations"),
		Method:        "post",
		Path:          "/api/3/action/organization_list",
		Summary:       "List organizations / publishers",
		Description:   "Returns all organizations (data publishers) on this portal.",
		RequestBody:   jsonBody(schema),
		InputSchema:   schema,
		StaticHeaders: ckanHeaders(),
	}
}

func listTags(api string) *canonical.Operation {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":         map[string]any{"type": "string", "description": "Filter tags by name prefix."},
			"all_fields":    map[string]any{"type": "boolean", "description": "Return full tag objects instead of just names."},
			"vocabulary_id": map[string]any{"type": "string", "description": "ID of a controlled vocabulary to search."},
			"limit":         map[string]any{"type": "integer", "description": "Maximum number of tags to return."},
			"offset":        map[string]any{"type": "integer", "description": "Offset for pagination."},
		},
		"additionalProperties": false,
	}
	return &canonical.Operation{
		ServiceName:   api,
		ID:            "listTags",
		ToolName:      canonical.ToolName(api, "listTags"),
		Method:        "post",
		Path:          "/api/3/action/tag_list",
		Summary:       "List tags",
		Description:   "Returns all tags used to categorize datasets on this portal.",
		RequestBody:   jsonBody(schema),
		InputSchema:   schema,
		StaticHeaders: ckanHeaders(),
	}
}
