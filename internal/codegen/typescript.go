package codegen

import (
	"fmt"
	"strings"
	"unicode"

	"skyline-mcp/internal/mcp"
)

// GenerateTypeScriptModule generates a TypeScript module from MCP tools
func GenerateTypeScriptModule(serviceName string, tools []*mcp.Tool) (map[string]string, error) {
	files := make(map[string]string)

	// Generate individual tool files
	toolNames := []string{}
	for _, tool := range tools {
		funcName, fileName := toolToFunctionName(tool.Name)
		tsCode, err := generateToolFunction(tool, funcName)
		if err != nil {
			return nil, fmt.Errorf("generate tool %s: %w", tool.Name, err)
		}
		files[fileName] = tsCode
		toolNames = append(toolNames, funcName)
	}

	// Generate index.ts that re-exports all tools
	files["index.ts"] = generateIndexFile(toolNames)

	// Generate client.ts (MCP tool caller)
	files["client.ts"] = generateClientFile()

	return files, nil
}

// toolToFunctionName converts MCP tool name to TypeScript function name
// Example: "nextcloud__files_sharing-shareapi-get-shares" → ("getShares", "getShares.ts")
func toolToFunctionName(toolName string) (funcName, fileName string) {
	// Remove service prefix (e.g., "nextcloud__")
	parts := strings.SplitN(toolName, "__", 2)
	if len(parts) == 2 {
		toolName = parts[1]
	}

	// Convert kebab-case to camelCase
	// "files_sharing-shareapi-get-shares" → "getShares"
	segments := strings.FieldsFunc(toolName, func(r rune) bool {
		return r == '-' || r == '_'
	})

	// Find the verb (get, create, delete, list, update)
	verbIdx := -1
	for i, seg := range segments {
		lower := strings.ToLower(seg)
		if lower == "get" || lower == "create" || lower == "delete" ||
			lower == "list" || lower == "update" || lower == "post" ||
			lower == "put" || lower == "patch" {
			verbIdx = i
			break
		}
	}

	var name strings.Builder
	if verbIdx >= 0 {
		// Start from verb
		for i := verbIdx; i < len(segments); i++ {
			if i == verbIdx {
				name.WriteString(strings.ToLower(segments[i]))
			} else {
				name.WriteString(capitalize(segments[i]))
			}
		}
	} else {
		// Fallback: use last segment
		if len(segments) > 0 {
			name.WriteString(strings.ToLower(segments[len(segments)-1]))
		} else {
			name.WriteString("call")
		}
	}

	funcName = name.String()
	fileName = funcName + ".ts"
	return
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// generateToolFunction generates TypeScript code for a single tool
func generateToolFunction(tool *mcp.Tool, funcName string) (string, error) {
	var b strings.Builder

	b.WriteString("import { callMCPTool } from '../client.ts';\n\n")

	// Generate input interface if tool has parameters
	hasInput := false
	if tool.InputSchema != nil {
		if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok && len(props) > 0 {
			hasInput = true
			b.WriteString(fmt.Sprintf("export interface %sInput {\n", capitalize(funcName)))
			
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
				optional := ""
				if !isRequired {
					optional = "?"
				}
				tsType := jsonSchemaDefToTypeScript(propDef)
				b.WriteString(fmt.Sprintf("  '%s'%s: %s;\n", propName, optional, tsType))
			}
			b.WriteString("}\n\n")
		}
	}

	// Generate function
	comment := strings.ReplaceAll(tool.Description, "\n", "\n * ")
	b.WriteString(fmt.Sprintf("/** %s */\n", comment))

	inputParam := ""
	if hasInput {
		inputParam = fmt.Sprintf("input: %sInput", capitalize(funcName))
	}

	b.WriteString(fmt.Sprintf("export function %s(%s): any {\n", funcName, inputParam))
	if hasInput {
		b.WriteString(fmt.Sprintf("  return callMCPTool('%s', input);\n", tool.Name))
	} else {
		b.WriteString(fmt.Sprintf("  return callMCPTool('%s', {});\n", tool.Name))
	}
	b.WriteString("}\n")

	return b.String(), nil
}

func jsonSchemaDefToTypeScript(propDef interface{}) string {
	propMap, ok := propDef.(map[string]interface{})
	if !ok {
		return "any"
	}
	
	typeStr, _ := propMap["type"].(string)
	switch typeStr {
	case "string":
		if enum, ok := propMap["enum"].([]interface{}); ok && len(enum) > 0 {
			enumStrs := []string{}
			for _, e := range enum {
				if s, ok := e.(string); ok {
					enumStrs = append(enumStrs, s)
				}
			}
			if len(enumStrs) > 0 {
				return fmt.Sprintf("'%s'", strings.Join(enumStrs, "' | '"))
			}
		}
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

// generateIndexFile generates index.ts that re-exports all tools
func generateIndexFile(toolNames []string) string {
	var b strings.Builder
	b.WriteString("// Auto-generated index file - re-exports all tools\n\n")
	for _, name := range toolNames {
		b.WriteString(fmt.Sprintf("export * from './%s.ts';\n", name))
	}
	return b.String()
}

// generateClientFile generates the MCP client.ts
// Host functions __callMCPTool and __searchTools are injected by the Go executor (goja).
func generateClientFile() string {
	return `// MCP Tool Client for embedded goja runtime
// Host functions are provided as globals by the Go executor

export function callMCPTool(toolName: string, args: any): any {
  return (globalThis as any).__callMCPTool(toolName, JSON.stringify(args));
}

export function searchTools(query: string, detail: string = 'name-and-description'): any[] {
  return (globalThis as any).__searchTools(query, detail);
}

export const __interfaces: string[] = (globalThis as any).__interfaces || [];
`
}
