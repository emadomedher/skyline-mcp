package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"skyline-mcp/internal/executor"
)

// HandleExecute handles POST /execute requests
func (s *Server) HandleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if code executor is configured
	if s.codeExecutor == nil {
		http.Error(w, "code execution not enabled", http.StatusNotImplemented)
		return
	}

	exec, ok := s.codeExecutor.(*executor.Executor)
	if !ok {
		http.Error(w, "invalid code executor", http.StatusInternalServerError)
		return
	}

	// Parse request
	var req executor.ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	slog.Info("running code", "component", "execute", "language", req.Language, "timeout", req.Timeout)

	// Execute code
	result, err := exec.Execute(r.Context(), req)
	if err != nil {
		slog.Error("execution failed", "component", "execute", "error", err)
		http.Error(w, fmt.Sprintf("execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	slog.Info("execution completed", "component", "execute", "exit_code", result.ExitCode, "execution_time", result.ExecutionTime)

	// Return result
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleInternalToolCall handles internal tool calls from executing code
// POST /internal/call-tool
func (s *Server) HandleInternalToolCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req executor.ToolCall
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	slog.Debug("internal tool call", "component", "execute", "tool", req.ToolName)

	// Find tool
	tool, exists := s.registry.Tools[req.ToolName]
	if !exists || tool == nil || tool.Operation == nil {
		result := executor.ToolCallResult{
			Error: fmt.Sprintf("tool not found: %s", req.ToolName),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}
	op := tool.Operation

	// Parse arguments
	var args map[string]interface{}
	if err := json.Unmarshal(req.Args, &args); err != nil {
		result := executor.ToolCallResult{
			Error: fmt.Sprintf("invalid arguments: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	// Execute tool via runtime executor
	runtimeResult, err := s.executor.Execute(r.Context(), op, args)
	if err != nil {
		result := executor.ToolCallResult{
			Error: fmt.Sprintf("tool execution failed: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	// Return result (extract body from runtime.Result)
	result := executor.ToolCallResult{
		Data: runtimeResult.Body,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleSearchTools handles POST /internal/search-tools requests
func (s *Server) HandleSearchTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req ToolSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	slog.Debug("search tools", "component", "execute", "query", req.Query, "detail", req.Detail)

	// Default detail level
	if req.Detail == "" {
		req.Detail = "name-and-description"
	}

	// Search tools
	results := SearchTools(s.registry, req.Query, req.Detail)

	slog.Debug("search tools completed", "component", "execute", "results", len(results))

	// Return results
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// HandleAgentPrompt handles GET /agent-prompt requests
func (s *Server) HandleAgentPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Generate agent prompt template
	prompt := GenerateAgentPromptTemplate(s.registry)

	// Return as plain text
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(prompt))
}
