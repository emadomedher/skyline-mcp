package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecuteRequest represents a code execution request
type ExecuteRequest struct {
	Code     string `json:"code"`
	Language string `json:"language"` // "typescript" or "python"
	Timeout  int    `json:"timeout"`  // seconds, default 30
}

// ExecuteResult represents the result of code execution
type ExecuteResult struct {
	Stdout        string   `json:"stdout"`
	Stderr        string   `json:"stderr"`
	ExitCode      int      `json:"exitCode"`
	ExecutionTime float64  `json:"executionTime"` // seconds
	ToolsCalled   []string `json:"toolsCalled"`
	Error         string   `json:"error,omitempty"`
}

// Executor runs user code in a sandboxed environment
type Executor struct {
	workspaceDir string
	mcpEndpoint  string
	interfaces   []string // Available service namespaces
}

// NewExecutor creates a new code executor
func NewExecutor(workspaceDir, mcpEndpoint string) *Executor {
	return &Executor{
		workspaceDir: workspaceDir,
		mcpEndpoint:  mcpEndpoint,
		interfaces:   []string{},
	}
}

// SetInterfaces updates the list of available service interfaces
func (e *Executor) SetInterfaces(interfaces []string) {
	e.interfaces = interfaces
}

// Execute runs user code with security constraints
func (e *Executor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	// Set default timeout
	if req.Timeout == 0 {
		req.Timeout = 30
	}

	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
	defer cancel()

	// Only TypeScript supported for now
	if req.Language != "typescript" && req.Language != "" {
		return &ExecuteResult{
			Error:    fmt.Sprintf("unsupported language: %s", req.Language),
			ExitCode: 1,
		}, nil
	}

	// Write user code to temp file
	codeFile := filepath.Join(e.workspaceDir, "user_code.ts")
	if err := os.WriteFile(codeFile, []byte(req.Code), 0644); err != nil {
		return nil, fmt.Errorf("write code file: %w", err)
	}
	defer os.Remove(codeFile)

	// Prepare Deno command with security constraints
	cmd := exec.CommandContext(execCtx, "deno", "run",
		"--allow-read="+e.workspaceDir,  // Only read workspace
		"--allow-env=MCP_INTERNAL_ENDPOINT,MCP_INTERFACES,MCP_SEARCH_ENDPOINT", // Discovery env vars
		"--allow-net=localhost,127.0.0.1",   // Only localhost network
		"--no-prompt",
		codeFile,
	)

	// Set MCP endpoint and interfaces environment variables
	interfacesJSON, _ := json.Marshal(e.interfaces)
	searchEndpoint := strings.Replace(e.mcpEndpoint, "/internal/call-tool", "/internal/search-tools", 1)
	cmd.Env = append(os.Environ(), 
		"MCP_INTERNAL_ENDPOINT="+e.mcpEndpoint,
		"MCP_INTERFACES="+string(interfacesJSON),
		"MCP_SEARCH_ENDPOINT="+searchEndpoint,
	)
	cmd.Dir = e.workspaceDir

	// Capture stdout/stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	// Start execution
	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start execution: %w", err)
	}

	// Read output
	stdout, _ := io.ReadAll(stdoutPipe)
	stderr, _ := io.ReadAll(stderrPipe)

	// Wait for completion
	err = cmd.Wait()
	executionTime := time.Since(startTime).Seconds()

	result := &ExecuteResult{
		Stdout:        string(stdout),
		Stderr:        string(stderr),
		ExecutionTime: executionTime,
		ToolsCalled:   []string{}, // TODO: Track from internal endpoint
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.Error = fmt.Sprintf("execution timeout after %ds", req.Timeout)
			result.ExitCode = 124 // Standard timeout exit code
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
			result.ExitCode = 1
		}
	}

	return result, nil
}

// SetupWorkspace creates the workspace directory structure with generated code
func (e *Executor) SetupWorkspace(serviceFiles map[string]map[string]string) error {
	// Create workspace directory
	if err := os.MkdirAll(e.workspaceDir, 0755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}

	// Create mcp/ directory
	mcpDir := filepath.Join(e.workspaceDir, "mcp")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		return fmt.Errorf("create mcp dir: %w", err)
	}

	// Write service files
	for serviceName, files := range serviceFiles {
		serviceDir := filepath.Join(mcpDir, serviceName)
		if err := os.MkdirAll(serviceDir, 0755); err != nil {
			return fmt.Errorf("create service dir %s: %w", serviceName, err)
		}

		for fileName, content := range files {
			filePath := filepath.Join(serviceDir, fileName)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				return fmt.Errorf("write file %s: %w", filePath, err)
			}
		}
	}

	return nil
}

// Validate checks if Deno is available
func ValidateDeno() error {
	cmd := exec.Command("deno", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("deno not found: %w (install from https://deno.land)", err)
	}
	return nil
}

// ToolCall represents an internal tool call from executing code
type ToolCall struct {
	ToolName string          `json:"toolName"`
	Args     json.RawMessage `json:"args"`
}

// ToolCallResult represents the result of an internal tool call
type ToolCallResult struct {
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}
