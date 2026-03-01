package codegen

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"skyline-mcp/internal/executor"
	"skyline-mcp/internal/mcp"
)

// SetupCodeExecution sets up code execution for the MCP server
// Returns the code executor if successful, or nil if code execution is not available
func SetupCodeExecution(registry *mcp.Registry, logger *slog.Logger) (*executor.Executor, error) {
	// Validate runtime (goja is always available since it's embedded)
	if err := executor.ValidateRuntime(); err != nil {
		logger.Debug("runtime not available, code execution disabled", "component", "codegen", "error", err)
		return nil, nil // Not an error, just disabled
	}

	logger.Debug("goja runtime available, enabling code execution", "component", "codegen")

	// Create workspace directory
	workspaceDir := filepath.Join(os.TempDir(), "skyline-workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return nil, fmt.Errorf("create workspace dir: %w", err)
	}

	// Generate TypeScript code for each service
	serviceFiles := make(map[string]map[string]string)

	// Convert map to slice
	var tools []*mcp.Tool
	for _, tool := range registry.Tools {
		tools = append(tools, tool)
	}

	// Group tools by service name (extract from tool name prefix)
	toolsByService := groupToolsByService(tools)

	for serviceName, svcTools := range toolsByService {
		logger.Debug("generating TypeScript for service", "component", "codegen", "service", serviceName, "tools", len(svcTools))
		files, err := GenerateTypeScriptModule(serviceName, svcTools)
		if err != nil {
			return nil, fmt.Errorf("generate typescript for %s: %w", serviceName, err)
		}
		serviceFiles[serviceName] = files
	}

	// Create executor
	mcpEndpoint := "http://localhost:8191/internal/call-tool"
	exec := executor.NewExecutor(workspaceDir, mcpEndpoint)

	// Setup workspace with generated files
	if err := exec.SetupWorkspace(serviceFiles); err != nil {
		return nil, fmt.Errorf("setup workspace: %w", err)
	}

	// Write shared client.ts at mcp/ level
	clientCode := generateClientFile()
	clientPath := filepath.Join(workspaceDir, "mcp", "client.ts")
	if err := os.WriteFile(clientPath, []byte(clientCode), 0644); err != nil {
		return nil, fmt.Errorf("write client.ts: %w", err)
	}

	// Set available interfaces
	interfaces := mcp.GetInterfacesList(registry)
	exec.SetInterfaces(interfaces)

	logger.Info("code execution workspace ready", "component", "codegen", "workspace", workspaceDir, "services", interfaces)

	return exec, nil
}

// groupToolsByService groups tools by their service name (prefix before __)
func groupToolsByService(tools []*mcp.Tool) map[string][]*mcp.Tool {
	serviceMap := make(map[string][]*mcp.Tool)

	for _, tool := range tools {
		// Extract service name from tool name
		// Example: "nextcloud__files-get" → "nextcloud"
		serviceName := "default"
		if idx := findDoubleUnderscore(tool.Name); idx >= 0 {
			serviceName = tool.Name[:idx]
		}

		serviceMap[serviceName] = append(serviceMap[serviceName], tool)
	}

	return serviceMap
}

func findDoubleUnderscore(s string) int {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '_' && s[i+1] == '_' {
			return i
		}
	}
	return -1
}
