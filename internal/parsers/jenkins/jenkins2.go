package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mcp-api-bridge/internal/canonical"
)

// Jenkins2Operations returns modern Jenkins 2.x API operations
// These are available in Jenkins 2.x and include Blue Ocean, Pipeline, and REST APIs
func ParseJenkins2ToCanonical(ctx context.Context, raw []byte, apiName, baseURLOverride string) (*canonical.Service, error) {
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

	// Detect Jenkins version from the response
	version := detectJenkinsVersion(raw)

	service := &canonical.Service{
		Name:    apiName,
		BaseURL: baseURL,
	}
	_ = version // Version detection for future use

	// Add all operations
	service.Operations = append(service.Operations, getCoreOperations(apiName)...)
	service.Operations = append(service.Operations, getJobOperations(apiName)...)
	service.Operations = append(service.Operations, getBuildOperations(apiName)...)
	service.Operations = append(service.Operations, getPipelineOperations(apiName)...)
	service.Operations = append(service.Operations, getQueueOperations(apiName)...)
	service.Operations = append(service.Operations, getNodeOperations(apiName)...)
	service.Operations = append(service.Operations, getCredentialsOperations(apiName)...)
	service.Operations = append(service.Operations, getPluginOperations(apiName)...)
	service.Operations = append(service.Operations, getBlueOceanOperations(apiName)...)
	service.Operations = append(service.Operations, getUserOperations(apiName)...)

	return service, nil
}

func detectJenkinsVersion(raw []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		// Try to extract version from various fields
		if version, ok := payload["version"].(string); ok {
			return version
		}
		// Jenkins /api/json doesn't always include version, but we can check for mode
		if mode, ok := payload["mode"].(string); ok && mode != "" {
			return "2.x (detected)"
		}
	}
	return "2.x"
}

// Core Jenkins Operations
func getCoreOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "getRoot",
			ToolName:    canonical.ToolName(apiName, "getRoot"),
			Method:      "get",
			Path:        "/api/json",
			Summary:     "Get Jenkins root object. Use tree=jobs[name,url,color] to list jobs.",
			Parameters: []canonical.Parameter{
				{Name: "tree", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Jenkins tree query to limit payload"}},
				{Name: "depth", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Depth of traversal"}},
			},
			InputSchema:   buildSimpleQuerySchema("Get Jenkins root", "tree", "depth"),
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "getObject",
			ToolName:    canonical.ToolName(apiName, "getObject"),
			Method:      "get",
			Path:        "/api/json",
			Summary:     "Get any Jenkins object by URL/path with optional tree filter.",
			Parameters: []canonical.Parameter{
				{Name: "url", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Jenkins object URL or path"}},
				{Name: "tree", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Jenkins tree query"}},
				{Name: "depth", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Depth of traversal"}},
			},
			InputSchema:     buildObjectQuerySchema(),
			StaticHeaders:   map[string]string{"Accept": "application/json"},
			DynamicURLParam: "url",
		},
		{
			ServiceName: apiName,
			ID:          "getVersion",
			ToolName:    canonical.ToolName(apiName, "getVersion"),
			Method:      "get",
			Path:        "/api/json",
			Summary:     "Get Jenkins version and system information.",
			Parameters:  []canonical.Parameter{},
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Job Management Operations
func getJobOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "listJobs",
			ToolName:    canonical.ToolName(apiName, "listJobs"),
			Method:      "get",
			Path:        "/api/json",
			Summary:     "List all Jenkins jobs with their status.",
			Parameters: []canonical.Parameter{
				{Name: "tree", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Tree query, default: jobs[name,url,color,lastBuild[number,url]]"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tree": map[string]any{"type": "string", "description": "Custom tree query"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "getJob",
			ToolName:    canonical.ToolName(apiName, "getJob"),
			Method:      "get",
			Path:        "/job/{jobName}/api/json",
			Summary:     "Get details about a specific Jenkins job.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
				{Name: "tree", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Tree query to filter response"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name"},
					"tree":    map[string]any{"type": "string", "description": "Tree query"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "createJob",
			ToolName:    canonical.ToolName(apiName, "createJob"),
			Method:      "post",
			Path:        "/createItem",
			Summary:     "Create a new Jenkins job from XML config.",
			Parameters: []canonical.Parameter{
				{Name: "name", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "New job name"}},
			},
			RequestBody: &canonical.RequestBody{
				Required:    true,
				ContentType: "application/xml",
				Schema:      map[string]any{"type": "string", "description": "Job XML configuration"},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "New job name"},
					"xml":  map[string]any{"type": "string", "description": "Job XML configuration"},
				},
				"required":             []string{"name", "xml"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/xml"},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "updateJobConfig",
			ToolName:    canonical.ToolName(apiName, "updateJobConfig"),
			Method:      "post",
			Path:        "/job/{jobName}/config.xml",
			Summary:     "Update an existing job's configuration.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
			},
			RequestBody: &canonical.RequestBody{
				Required:    true,
				ContentType: "application/xml",
				Schema:      map[string]any{"type": "string", "description": "Updated job XML configuration"},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name"},
					"xml":     map[string]any{"type": "string", "description": "Job XML configuration"},
				},
				"required":             []string{"jobName", "xml"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/xml"},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "getJobConfig",
			ToolName:    canonical.ToolName(apiName, "getJobConfig"),
			Method:      "get",
			Path:        "/job/{jobName}/config.xml",
			Summary:     "Get a job's XML configuration.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/xml"},
		},
		{
			ServiceName: apiName,
			ID:          "deleteJob",
			ToolName:    canonical.ToolName(apiName, "deleteJob"),
			Method:      "post",
			Path:        "/job/{jobName}/doDelete",
			Summary:     "Delete a Jenkins job.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name to delete"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "copyJob",
			ToolName:    canonical.ToolName(apiName, "copyJob"),
			Method:      "post",
			Path:        "/createItem",
			Summary:     "Copy an existing job to a new job.",
			Parameters: []canonical.Parameter{
				{Name: "name", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "New job name"}},
				{Name: "mode", In: "query", Required: true, Schema: map[string]any{"type": "string", "enum": []string{"copy"}}},
				{Name: "from", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Source job name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "New job name"},
					"from": map[string]any{"type": "string", "description": "Source job name"},
				},
				"required":             []string{"name", "from"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "enableJob",
			ToolName:    canonical.ToolName(apiName, "enableJob"),
			Method:      "post",
			Path:        "/job/{jobName}/enable",
			Summary:     "Enable a disabled Jenkins job.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name to enable"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "disableJob",
			ToolName:    canonical.ToolName(apiName, "disableJob"),
			Method:      "post",
			Path:        "/job/{jobName}/disable",
			Summary:     "Disable a Jenkins job.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name to disable"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
	}
}

// Build Operations
func getBuildOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "triggerBuild",
			ToolName:    canonical.ToolName(apiName, "triggerBuild"),
			Method:      "post",
			Path:        "/job/{jobName}/build",
			Summary:     "Trigger a build for a job (no parameters).",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
				{Name: "delay", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Delay in seconds before build starts"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name"},
					"delay":   map[string]any{"type": "integer", "description": "Optional delay in seconds"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "triggerBuildWithParameters",
			ToolName:    canonical.ToolName(apiName, "triggerBuildWithParameters"),
			Method:      "post",
			Path:        "/job/{jobName}/buildWithParameters",
			Summary:     "Trigger a parameterized build. Include parameters as query params.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName":    map[string]any{"type": "string", "description": "Job name"},
					"parameters": map[string]any{"type": "object", "additionalProperties": true, "description": "Build parameters as key-value pairs"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			QueryParamsObject: "parameters",
			RequiresCrumb:     true,
		},
		{
			ServiceName: apiName,
			ID:          "getBuild",
			ToolName:    canonical.ToolName(apiName, "getBuild"),
			Method:      "get",
			Path:        "/job/{jobName}/{buildNumber}/api/json",
			Summary:     "Get details about a specific build.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
				{Name: "buildNumber", In: "path", Required: true, Schema: map[string]any{"type": "integer", "description": "Build number or 'lastBuild'"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName":     map[string]any{"type": "string", "description": "Job name"},
					"buildNumber": map[string]any{"type": "string", "description": "Build number or 'lastBuild', 'lastSuccessfulBuild', etc."},
				},
				"required":             []string{"jobName", "buildNumber"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "stopBuild",
			ToolName:    canonical.ToolName(apiName, "stopBuild"),
			Method:      "post",
			Path:        "/job/{jobName}/{buildNumber}/stop",
			Summary:     "Stop a running build.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
				{Name: "buildNumber", In: "path", Required: true, Schema: map[string]any{"type": "integer", "description": "Build number"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName":     map[string]any{"type": "string", "description": "Job name"},
					"buildNumber": map[string]any{"type": "integer", "description": "Build number"},
				},
				"required":             []string{"jobName", "buildNumber"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "getBuildLog",
			ToolName:    canonical.ToolName(apiName, "getBuildLog"),
			Method:      "get",
			Path:        "/job/{jobName}/{buildNumber}/consoleText",
			Summary:     "Get console output (log) for a build.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
				{Name: "buildNumber", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Build number or 'lastBuild'"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName":     map[string]any{"type": "string", "description": "Job name"},
					"buildNumber": map[string]any{"type": "string", "description": "Build number or 'lastBuild'"},
				},
				"required":             []string{"jobName", "buildNumber"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "text/plain"},
		},
		{
			ServiceName: apiName,
			ID:          "getLastBuild",
			ToolName:    canonical.ToolName(apiName, "getLastBuild"),
			Method:      "get",
			Path:        "/job/{jobName}/lastBuild/api/json",
			Summary:     "Get the last build information for a job.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName": map[string]any{"type": "string", "description": "Job name"},
				},
				"required":             []string{"jobName"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "getBuildArtifacts",
			ToolName:    canonical.ToolName(apiName, "getBuildArtifacts"),
			Method:      "get",
			Path:        "/job/{jobName}/{buildNumber}/artifact/*zip*/archive.zip",
			Summary:     "Download all build artifacts as a ZIP file.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Job name"}},
				{Name: "buildNumber", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Build number"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName":     map[string]any{"type": "string", "description": "Job name"},
					"buildNumber": map[string]any{"type": "string", "description": "Build number"},
				},
				"required":             []string{"jobName", "buildNumber"},
				"additionalProperties": false,
			},
		},
	}
}

// Pipeline Operations (Jenkins 2.x Pipeline Plugin)
func getPipelineOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "createPipeline",
			ToolName:    canonical.ToolName(apiName, "createPipeline"),
			Method:      "post",
			Path:        "/createItem",
			Summary:     "Create a new Pipeline job with a Jenkinsfile script.",
			Parameters: []canonical.Parameter{
				{Name: "name", In: "query", Required: true, Schema: map[string]any{"type": "string", "description": "Pipeline name"}},
			},
			RequestBody: &canonical.RequestBody{
				Required:    true,
				ContentType: "application/xml",
				Schema:      map[string]any{"type": "string", "description": "Pipeline XML config with Jenkinsfile"},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":       map[string]any{"type": "string", "description": "Pipeline name"},
					"jenkinsfile": map[string]any{"type": "string", "description": "Jenkinsfile content (Groovy script)"},
				},
				"required":             []string{"name", "jenkinsfile"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Content-Type": "application/xml"},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "replayPipeline",
			ToolName:    canonical.ToolName(apiName, "replayPipeline"),
			Method:      "post",
			Path:        "/job/{jobName}/{buildNumber}/replay",
			Summary:     "Replay a pipeline build with modified Jenkinsfile.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Pipeline job name"}},
				{Name: "buildNumber", In: "path", Required: true, Schema: map[string]any{"type": "integer", "description": "Build number"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName":     map[string]any{"type": "string", "description": "Pipeline job name"},
					"buildNumber": map[string]any{"type": "integer", "description": "Build number to replay"},
				},
				"required":             []string{"jobName", "buildNumber"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "getPipelineStages",
			ToolName:    canonical.ToolName(apiName, "getPipelineStages"),
			Method:      "get",
			Path:        "/job/{jobName}/{buildNumber}/wfapi/describe",
			Summary:     "Get Pipeline stages and status from Workflow API.",
			Parameters: []canonical.Parameter{
				{Name: "jobName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Pipeline job name"}},
				{Name: "buildNumber", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Build number"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"jobName":     map[string]any{"type": "string", "description": "Pipeline job name"},
					"buildNumber": map[string]any{"type": "string", "description": "Build number or 'lastBuild'"},
				},
				"required":             []string{"jobName", "buildNumber"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Queue Operations
func getQueueOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "getQueue",
			ToolName:    canonical.ToolName(apiName, "getQueue"),
			Method:      "get",
			Path:        "/queue/api/json",
			Summary:     "Get the build queue.",
			Parameters:  []canonical.Parameter{},
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "cancelQueueItem",
			ToolName:    canonical.ToolName(apiName, "cancelQueueItem"),
			Method:      "post",
			Path:        "/queue/cancelItem",
			Summary:     "Cancel a queued build item.",
			Parameters: []canonical.Parameter{
				{Name: "id", In: "query", Required: true, Schema: map[string]any{"type": "integer", "description": "Queue item ID"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "Queue item ID to cancel"},
				},
				"required":             []string{"id"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
	}
}

// Node/Agent Operations
func getNodeOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "listNodes",
			ToolName:    canonical.ToolName(apiName, "listNodes"),
			Method:      "get",
			Path:        "/computer/api/json",
			Summary:     "List all Jenkins nodes/agents and their status.",
			Parameters: []canonical.Parameter{
				{Name: "depth", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Depth of traversal"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"depth": map[string]any{"type": "integer", "description": "Depth of traversal"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "getNode",
			ToolName:    canonical.ToolName(apiName, "getNode"),
			Method:      "get",
			Path:        "/computer/{nodeName}/api/json",
			Summary:     "Get details about a specific node/agent.",
			Parameters: []canonical.Parameter{
				{Name: "nodeName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Node name (or 'master' for controller)"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodeName": map[string]any{"type": "string", "description": "Node name"},
				},
				"required":             []string{"nodeName"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "markNodeOffline",
			ToolName:    canonical.ToolName(apiName, "markNodeOffline"),
			Method:      "post",
			Path:        "/computer/{nodeName}/toggleOffline",
			Summary:     "Mark a node offline with an optional reason.",
			Parameters: []canonical.Parameter{
				{Name: "nodeName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Node name"}},
				{Name: "offlineMessage", In: "query", Required: false, Schema: map[string]any{"type": "string", "description": "Reason for offline"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodeName":       map[string]any{"type": "string", "description": "Node name"},
					"offlineMessage": map[string]any{"type": "string", "description": "Offline reason"},
				},
				"required":             []string{"nodeName"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
		{
			ServiceName: apiName,
			ID:          "deleteNode",
			ToolName:    canonical.ToolName(apiName, "deleteNode"),
			Method:      "post",
			Path:        "/computer/{nodeName}/doDelete",
			Summary:     "Delete a Jenkins node/agent.",
			Parameters: []canonical.Parameter{
				{Name: "nodeName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Node name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodeName": map[string]any{"type": "string", "description": "Node name to delete"},
				},
				"required":             []string{"nodeName"},
				"additionalProperties": false,
			},
			RequiresCrumb: true,
		},
	}
}

// Credentials Operations (Credentials Plugin)
func getCredentialsOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "listCredentials",
			ToolName:    canonical.ToolName(apiName, "listCredentials"),
			Method:      "get",
			Path:        "/credentials/api/json",
			Summary:     "List all credential stores and domains (requires Credentials Plugin).",
			Parameters:  []canonical.Parameter{},
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Plugin Operations
func getPluginOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "listPlugins",
			ToolName:    canonical.ToolName(apiName, "listPlugins"),
			Method:      "get",
			Path:        "/pluginManager/api/json",
			Summary:     "List all installed Jenkins plugins.",
			Parameters: []canonical.Parameter{
				{Name: "depth", In: "query", Required: false, Schema: map[string]any{"type": "integer", "description": "Depth of traversal"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"depth": map[string]any{"type": "integer", "description": "Depth of traversal"},
				},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Blue Ocean API Operations (Blue Ocean Plugin)
func getBlueOceanOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "blueOceanPipelines",
			ToolName:    canonical.ToolName(apiName, "blueOceanPipelines"),
			Method:      "get",
			Path:        "/blue/rest/organizations/jenkins/pipelines/",
			Summary:     "List all pipelines via Blue Ocean API.",
			Parameters:  []canonical.Parameter{},
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "blueOceanRuns",
			ToolName:    canonical.ToolName(apiName, "blueOceanRuns"),
			Method:      "get",
			Path:        "/blue/rest/organizations/jenkins/pipelines/{pipelineName}/runs/",
			Summary:     "List all runs for a pipeline via Blue Ocean API.",
			Parameters: []canonical.Parameter{
				{Name: "pipelineName", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Pipeline name"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pipelineName": map[string]any{"type": "string", "description": "Pipeline name"},
				},
				"required":             []string{"pipelineName"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// User Operations
func getUserOperations(apiName string) []*canonical.Operation {
	return []*canonical.Operation{
		{
			ServiceName: apiName,
			ID:          "getCurrentUser",
			ToolName:    canonical.ToolName(apiName, "getCurrentUser"),
			Method:      "get",
			Path:        "/me/api/json",
			Summary:     "Get information about the current authenticated user.",
			Parameters:  []canonical.Parameter{},
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
		{
			ServiceName: apiName,
			ID:          "getUser",
			ToolName:    canonical.ToolName(apiName, "getUser"),
			Method:      "get",
			Path:        "/user/{username}/api/json",
			Summary:     "Get information about a specific user.",
			Parameters: []canonical.Parameter{
				{Name: "username", In: "path", Required: true, Schema: map[string]any{"type": "string", "description": "Username"}},
			},
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"username": map[string]any{"type": "string", "description": "Username"},
				},
				"required":             []string{"username"},
				"additionalProperties": false,
			},
			StaticHeaders: map[string]string{"Accept": "application/json"},
		},
	}
}

// Helper functions
func buildSimpleQuerySchema(description string, params ...string) map[string]any {
	props := make(map[string]any)
	for _, param := range params {
		props[param] = map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("Query parameter: %s", param),
		}
	}
	return map[string]any{
		"type":                 "object",
		"description":          description,
		"properties":           props,
		"additionalProperties": false,
	}
}

func buildObjectQuerySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Jenkins object URL or path",
			},
			"tree": map[string]any{
				"type":        "string",
				"description": "Optional tree query",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": "Optional depth",
			},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}
