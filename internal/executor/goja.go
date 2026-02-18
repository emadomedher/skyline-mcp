package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
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

// Executor runs user code in a sandboxed goja JavaScript runtime
type Executor struct {
	workspaceDir string
	mcpEndpoint  string
	interfaces   []string
	callToolFn   func(ctx context.Context, toolName string, args map[string]any) (any, error)
}

// NewExecutor creates a new code executor
func NewExecutor(workspaceDir, mcpEndpoint string) *Executor {
	return &Executor{
		workspaceDir: workspaceDir,
		mcpEndpoint:  mcpEndpoint,
	}
}

// SetInterfaces updates the list of available service interfaces
func (e *Executor) SetInterfaces(interfaces []string) {
	e.interfaces = interfaces
}

// SetDirectCallFunc sets a function for direct tool calling (bypasses HTTP).
// Used in STDIO mode where no HTTP server is running.
func (e *Executor) SetDirectCallFunc(fn func(ctx context.Context, toolName string, args map[string]any) (any, error)) {
	e.callToolFn = fn
}

// transpileAndBundle uses esbuild to bundle TypeScript into a single JavaScript IIFE.
// This resolves all imports relative to the entry point's directory.
func transpileAndBundle(entryPoint string) (string, error) {
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{entryPoint},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatIIFE,
		Target:      api.ES2020,
		Platform:    api.PlatformNeutral,
		LogLevel:    api.LogLevelSilent,
	})

	if len(result.Errors) > 0 {
		var errs []string
		for _, e := range result.Errors {
			errs = append(errs, e.Text)
		}
		return "", fmt.Errorf("build errors: %s", strings.Join(errs, "; "))
	}

	if len(result.OutputFiles) == 0 {
		return "", fmt.Errorf("no output from esbuild")
	}

	return string(result.OutputFiles[0].Contents), nil
}

// Execute runs user code with security constraints using goja.
// TypeScript is transpiled via esbuild, then executed in a sandboxed goja VM
// with only console, callMCPTool, searchTools, and restricted fetch available.
func (e *Executor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	if req.Timeout == 0 {
		req.Timeout = 30
	}

	if req.Language != "typescript" && req.Language != "" {
		return &ExecuteResult{
			Error:    fmt.Sprintf("unsupported language: %s", req.Language),
			ExitCode: 1,
		}, nil
	}

	// Wrap user code in async IIFE for top-level await support.
	// Even though our host functions are synchronous, user code may use
	// await syntax which requires an async context.
	wrappedCode := "(async () => {\n" + req.Code + "\n})();\n"

	// Write to temp file for esbuild bundling (resolves imports from workspace)
	codeFile := filepath.Join(e.workspaceDir, "user_code.ts")
	if err := os.WriteFile(codeFile, []byte(wrappedCode), 0644); err != nil {
		return nil, fmt.Errorf("write code file: %w", err)
	}
	defer os.Remove(codeFile)

	// Bundle with esbuild (resolves imports, transpiles TS→JS)
	js, err := transpileAndBundle(codeFile)
	if err != nil {
		return &ExecuteResult{
			Error:    fmt.Sprintf("transpile error: %v", err),
			ExitCode: 1,
		}, nil
	}

	// Create goja runtime (fresh VM per request — no shared state)
	vm := goja.New()

	var stdout, stderr bytes.Buffer
	var toolsCalled []string

	// Register console.log/warn/error
	registerConsole(vm, &stdout, &stderr)

	// Register __callMCPTool (synchronous Go function called from JS)
	vm.Set("__callMCPTool", func(call goja.FunctionCall) goja.Value {
		toolName := call.Argument(0).String()
		argsJSON := call.Argument(1).String()

		toolsCalled = append(toolsCalled, toolName)

		var args map[string]any
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			panic(vm.NewGoError(fmt.Errorf("invalid args JSON: %w", err)))
		}

		var result any
		var callErr error

		if e.callToolFn != nil {
			result, callErr = e.callToolFn(ctx, toolName, args)
		} else {
			result, callErr = e.httpCallTool(ctx, toolName, args)
		}

		if callErr != nil {
			panic(vm.NewGoError(callErr))
		}

		return vm.ToValue(result)
	})

	// Register __searchTools
	vm.Set("__searchTools", func(call goja.FunctionCall) goja.Value {
		query := call.Argument(0).String()
		detail := "name-and-description"
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			detail = call.Argument(1).String()
		}

		result, err := e.httpSearchTools(ctx, query, detail)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		return vm.ToValue(result)
	})

	// Set __interfaces
	vm.Set("__interfaces", e.interfaces)

	// Register restricted fetch (localhost only)
	registerFetch(vm, ctx)

	// Set execution timeout via interrupt (runs in a separate goroutine)
	timer := time.AfterFunc(time.Duration(req.Timeout)*time.Second, func() {
		vm.Interrupt("execution timeout")
	})
	defer timer.Stop()

	// Execute the bundled JavaScript
	startTime := time.Now()
	_, runErr := vm.RunString(js)
	executionTime := time.Since(startTime).Seconds()

	result := &ExecuteResult{
		Stdout:        stdout.String(),
		Stderr:        stderr.String(),
		ExecutionTime: executionTime,
		ToolsCalled:   toolsCalled,
	}

	if runErr != nil {
		if strings.Contains(runErr.Error(), "execution timeout") {
			result.Error = fmt.Sprintf("execution timeout after %ds", req.Timeout)
			result.ExitCode = 124
		} else {
			result.Error = runErr.Error()
			result.ExitCode = 1
		}
	}

	return result, nil
}

// registerConsole sets up console.log/warn/error on the goja runtime
func registerConsole(vm *goja.Runtime, stdout, stderr *bytes.Buffer) {
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		stdout.WriteString(formatJSArgs(call) + "\n")
		return goja.Undefined()
	})
	console.Set("warn", func(call goja.FunctionCall) goja.Value {
		stderr.WriteString(formatJSArgs(call) + "\n")
		return goja.Undefined()
	})
	console.Set("error", func(call goja.FunctionCall) goja.Value {
		stderr.WriteString(formatJSArgs(call) + "\n")
		return goja.Undefined()
	})
	vm.Set("console", console)
}

// registerFetch sets up a restricted fetch function (localhost only).
// Returns a synchronous response object with .text() and .json() methods.
func registerFetch(vm *goja.Runtime, ctx context.Context) {
	vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()

		// Security: restrict to localhost only
		if !strings.HasPrefix(url, "http://localhost") &&
			!strings.HasPrefix(url, "http://127.0.0.1") &&
			!strings.HasPrefix(url, "https://localhost") &&
			!strings.HasPrefix(url, "https://127.0.0.1") {
			panic(vm.NewGoError(fmt.Errorf("fetch restricted to localhost, got: %s", url)))
		}

		method := "GET"
		var body io.Reader
		headers := make(map[string]string)

		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
			opts := call.Argument(1).Export()
			if optsMap, ok := opts.(map[string]any); ok {
				if m, ok := optsMap["method"].(string); ok {
					method = strings.ToUpper(m)
				}
				if b, ok := optsMap["body"].(string); ok {
					body = strings.NewReader(b)
				}
				if h, ok := optsMap["headers"].(map[string]any); ok {
					for k, v := range h {
						if vs, ok := v.(string); ok {
							headers[k] = vs
						}
					}
				}
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		respObj := vm.NewObject()
		respObj.Set("ok", resp.StatusCode >= 200 && resp.StatusCode < 300)
		respObj.Set("status", resp.StatusCode)
		respObj.Set("statusText", http.StatusText(resp.StatusCode))
		bodyStr := string(respBody)
		respObj.Set("text", func(goja.FunctionCall) goja.Value {
			return vm.ToValue(bodyStr)
		})
		respObj.Set("json", func(goja.FunctionCall) goja.Value {
			var v any
			if err := json.Unmarshal(respBody, &v); err != nil {
				panic(vm.NewGoError(fmt.Errorf("invalid JSON response: %w", err)))
			}
			return vm.ToValue(v)
		})

		return respObj
	})
}

// formatJSArgs formats goja function call arguments for console output
func formatJSArgs(call goja.FunctionCall) string {
	parts := make([]string, len(call.Arguments))
	for i, arg := range call.Arguments {
		if goja.IsUndefined(arg) {
			parts[i] = "undefined"
		} else if goja.IsNull(arg) {
			parts[i] = "null"
		} else {
			exported := arg.Export()
			switch v := exported.(type) {
			case string:
				parts[i] = v
			default:
				if b, err := json.Marshal(exported); err == nil {
					parts[i] = string(b)
				} else {
					parts[i] = arg.String()
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

// httpCallTool calls a tool via the internal HTTP endpoint
func (e *Executor) httpCallTool(ctx context.Context, toolName string, args map[string]any) (any, error) {
	payload, _ := json.Marshal(map[string]any{
		"toolName": toolName,
		"args":     args,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.mcpEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call tool: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result ToolCallResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("tool error: %s", result.Error)
	}

	return result.Data, nil
}

// httpSearchTools searches tools via the internal HTTP endpoint
func (e *Executor) httpSearchTools(ctx context.Context, query, detail string) (any, error) {
	searchEndpoint := strings.Replace(e.mcpEndpoint, "/internal/call-tool", "/internal/search-tools", 1)

	payload, _ := json.Marshal(map[string]any{
		"query":  query,
		"detail": detail,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search tools: %w", err)
	}
	defer resp.Body.Close()

	var results []any
	json.NewDecoder(resp.Body).Decode(&results)

	return results, nil
}

// SetupWorkspace creates the workspace directory structure with generated code
func (e *Executor) SetupWorkspace(serviceFiles map[string]map[string]string) error {
	if err := os.MkdirAll(e.workspaceDir, 0755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}

	mcpDir := filepath.Join(e.workspaceDir, "mcp")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		return fmt.Errorf("create mcp dir: %w", err)
	}

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

// ValidateRuntime checks if the execution runtime is available.
// Always returns nil since goja is embedded in the binary.
func ValidateRuntime() error {
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
