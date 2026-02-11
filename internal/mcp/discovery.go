package mcp

import (
	"strings"
)

// ToolSearchResult represents a search result for tools
type ToolSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Service     string `json:"service"`
	Interface   string `json:"interface,omitempty"` // Full TypeScript interface (optional)
}

// SearchTools searches for tools matching a query
func SearchTools(registry *Registry, query string, detail string) []ToolSearchResult {
	query = strings.ToLower(query)
	results := []ToolSearchResult{}

	for _, tool := range registry.Tools {
		// Extract service name
		serviceName := "default"
		if idx := strings.Index(tool.Name, "__"); idx >= 0 {
			serviceName = tool.Name[:idx]
		}

		// Check if query matches tool name or description
		nameMatch := strings.Contains(strings.ToLower(tool.Name), query)
		descMatch := strings.Contains(strings.ToLower(tool.Description), query)
		serviceMatch := strings.Contains(strings.ToLower(serviceName), query)

		if nameMatch || descMatch || serviceMatch {
			result := ToolSearchResult{
				Name:        tool.Name,
				Description: tool.Description,
				Service:     serviceName,
			}

			// Include full interface if requested
			if detail == "full" {
				result.Interface = generateToolInterface(tool)
			}

			results = append(results, result)
		}
	}

	return results
}

// GetTruncatedToolList generates a truncated list of all tools (60 chars per description)
func GetTruncatedToolList(registry *Registry) string {
	var b strings.Builder
	b.WriteString("Available tools (truncated):\n\n")

	// Group by service
	serviceTools := make(map[string][]*Tool)
	for _, tool := range registry.Tools {
		serviceName := "default"
		if idx := strings.Index(tool.Name, "__"); idx >= 0 {
			serviceName = tool.Name[:idx]
		}
		serviceTools[serviceName] = append(serviceTools[serviceName], tool)
	}

	// Write grouped list
	for serviceName, tools := range serviceTools {
		b.WriteString("## ")
		b.WriteString(serviceName)
		b.WriteString("\n")

		for _, tool := range tools {
			// Extract function name
			funcName := tool.Name
			if idx := strings.Index(tool.Name, "__"); idx >= 0 {
				funcName = tool.Name[idx+2:]
			}

			// Truncate description to 60 chars
			desc := tool.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}

			b.WriteString("- ")
			b.WriteString(funcName)
			b.WriteString(": ")
			b.WriteString(desc)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("\nUse searchTools('query') to find specific tools with full details.\n")
	return b.String()
}

// GetInterfacesList returns list of available service namespaces
func GetInterfacesList(registry *Registry) []string {
	services := make(map[string]bool)

	for _, tool := range registry.Tools {
		serviceName := "default"
		if idx := strings.Index(tool.Name, "__"); idx >= 0 {
			serviceName = tool.Name[:idx]
		}
		services[serviceName] = true
	}

	result := []string{}
	for service := range services {
		result = append(result, service)
	}
	return result
}

// GetToolInterface returns the TypeScript interface for a specific tool
func GetToolInterface(registry *Registry, toolName string) string {
	tool, exists := registry.Tools[toolName]
	if !exists {
		return ""
	}
	return generateToolInterface(tool)
}

// generateToolInterface creates TypeScript interface definition for a tool
func generateToolInterface(tool *Tool) string {
	var b strings.Builder

	// Generate input interface
	if tool.InputSchema != nil {
		if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok && len(props) > 0 {
			b.WriteString("interface Input {\n")

			// Get required fields
			required := []string{}
			if req, ok := tool.InputSchema["required"].([]interface{}); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						required = append(required, s)
					}
				}
			}

			for propName, propDef := range props {
				isRequired := contains(required, propName)
				optional := "?"
				if isRequired {
					optional = ""
				}

				tsType := "any"
				if propMap, ok := propDef.(map[string]interface{}); ok {
					if typeStr, ok := propMap["type"].(string); ok {
						tsType = jsonTypeToTS(typeStr)
					}
					if desc, ok := propMap["description"].(string); ok {
						b.WriteString("  /** ")
						b.WriteString(desc)
						b.WriteString(" */\n")
					}
				}

				b.WriteString("  ")
				b.WriteString(propName)
				b.WriteString(optional)
				b.WriteString(": ")
				b.WriteString(tsType)
				b.WriteString(";\n")
			}
			b.WriteString("}\n")
		}
	}

	return b.String()
}

func jsonTypeToTS(jsonType string) string {
	switch jsonType {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		return "any[]"
	case "object":
		return "any"
	default:
		return "any"
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GenerateAgentPromptTemplate creates the prompt template for AI agents
func GenerateAgentPromptTemplate(registry *Registry) string {
	truncatedList := GetTruncatedToolList(registry)

	return `# Code Execution with MCP Tools

You have access to a code execution environment with MCP tools.

## Available Tools

` + truncatedList + `

## How to Use Tools

### 1. Search for Tools
` + "```typescript" + `
const tools = await searchTools('medical history');
console.log(tools);
// Returns: [{ name: 'getPetMedicalHistory', description: '...', service: 'pets' }]
` + "```" + `

### 2. Import and Use Tools
` + "```typescript" + `
import { getPetMedicalHistory } from './mcp/pets/getPetMedicalHistory.ts';
const history = await getPetMedicalHistory({ petName: 'Rufus' });
console.log(history);
` + "```" + `

### 3. Runtime Introspection
` + "```typescript" + `
// List all available services
console.log(__interfaces);
// Returns: ['nextcloud', 'gitlab', 'pets', ...]

// Get tool interface details
const schema = __getToolInterface('nextcloud__getShares');
console.log(schema);
` + "```" + `

## Best Practices

1. **Search First**: Use searchTools() to find relevant tools before importing
2. **Handle Errors**: Wrap tool calls in try/catch blocks
3. **Process Data Locally**: Filter and transform results in code, not in context
4. **Log Progress**: Use console.log() to show what you're doing
5. **Return Summaries**: Return concise results, not raw API responses

## Example Workflow

` + "```typescript" + `
// 1. Find relevant tools
const tools = await searchTools('file sharing');
console.log('Found tools:', tools.map(t => t.name));

// 2. Import what you need
import { search, createShare } from './mcp/nextcloud/index.ts';

// 3. Execute workflow
const files = await search({ name: 'presentation.pptx' });
const target = files.find(f => f.name === 'presentation.pptx');

await createShare({
  fileId: target.id,
  shareWith: 'bob'
});

console.log('Shared presentation with Bob');
` + "```" + `

## Security

- All code runs in a sandboxed environment
- Only MCP tools are accessible (no filesystem, no external network)
- 30-second timeout enforced
- Tools require proper authentication

Write efficient, clear code that accomplishes the user's request.
`
}

// ToolSearchRequest for the searchTools endpoint
type ToolSearchRequest struct {
	Query   string `json:"query"`
	Service string `json:"service,omitempty"`
	Detail  string `json:"detail,omitempty"` // "name-only", "name-and-description", "full"
}
