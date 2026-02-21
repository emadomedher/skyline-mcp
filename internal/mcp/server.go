package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"time"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
)

// ToolCallEvent contains information about a completed tool call, used for stats/audit.
type ToolCallEvent struct {
	SessionID    string
	ToolName     string
	APIName      string
	Arguments    map[string]any
	Duration     time.Duration
	Success      bool
	ErrorMsg     string
	RequestSize  int64
	ResponseSize int64
}

// ToolCallHook is called after every tools/call execution for audit and metrics.
type ToolCallHook func(ctx context.Context, event ToolCallEvent)

// ToolCallStartEvent is fired before tool execution begins (for real-time activity tracking).
type ToolCallStartEvent struct {
	SessionID string
	ToolName  string
	APIName   string
}

// ToolCallStartHook is called before tool execution begins.
type ToolCallStartHook func(ctx context.Context, event ToolCallStartEvent)

const protocolVersion = "2025-11-25"

// Executor interface for executing operations
type Executor interface {
	Execute(ctx context.Context, op *canonical.Operation, args map[string]any) (*runtime.Result, error)
}

type Server struct {
	registry           *Registry
	executor           Executor    // Runtime executor for tool calls
	codeExecutor       interface{} // Code executor for /execute endpoint (optional)
	version            string
	logger             *log.Logger
	redactor           *redact.Redactor
	toolCallHook       ToolCallHook      // Optional hook for audit/metrics on tool calls
	toolCallStartHook  ToolCallStartHook // Optional hook fired before tool execution
	maxResponseBytes   int               // Default max response size in bytes (0 = no limit)
	maxResponseByAPI   map[string]int    // Per-API max response bytes (overrides default)
}

func NewServer(registry *Registry, executor Executor, logger *log.Logger, redactor *redact.Redactor, version string) *Server {
	if version == "" {
		version = "dev"
	}
	return &Server{
		registry: registry,
		executor: executor,
		version:  version,
		logger:   logger,
		redactor: redactor,
	}
}

// SetCodeExecutor sets the code executor for /execute endpoint
func (s *Server) SetCodeExecutor(exec interface{}) {
	s.codeExecutor = exec
}

// SetToolCallHook sets a callback that fires after every tools/call execution.
func (s *Server) SetToolCallHook(hook ToolCallHook) {
	s.toolCallHook = hook
}

// SetToolCallStartHook sets a callback that fires before tool execution begins.
func (s *Server) SetToolCallStartHook(hook ToolCallStartHook) {
	s.toolCallStartHook = hook
}

// SetMaxResponseBytes sets the default maximum response size for tool call results.
func (s *Server) SetMaxResponseBytes(maxBytes int) {
	s.maxResponseBytes = maxBytes
}

// SetMaxResponseBytesByAPI sets per-API maximum response sizes, overriding the default.
func (s *Server) SetMaxResponseBytesByAPI(m map[string]int) {
	s.maxResponseByAPI = m
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)

	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		resp := s.handleRequest(ctx, &req)
		if resp == nil {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

// HandleRequest handles a single MCP JSON-RPC request (exported for HTTP transport)
func (s *Server) HandleRequest(ctx context.Context, req *rpcRequest) *rpcResponse {
	return s.handleRequest(ctx, req)
}

func (s *Server) handleRequest(ctx context.Context, req *rpcRequest) *rpcResponse {
	if req.Jsonrpc != "2.0" {
		return rpcErrorResponse(req.ID, -32600, "invalid jsonrpc version", nil)
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		// Notification; no response.
		return nil
	}

	switch req.Method {
	case "initialize":
		return rpcSuccess(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools":     map[string]any{"list": true, "call": true},
				"resources": map[string]any{"list": true, "read": true},
			},
			"serverInfo": map[string]any{
				"name":    "Skyline MCP",
				"version": s.version,
			},
		})
	case "tools/list":
		return s.handleListTools(req.ID)
	case "tools/call":
		return s.handleCallTool(ctx, req.ID, req.Params)
	case "resources/list":
		return s.handleListResources(req.ID)
	case "resources/read":
		return s.handleReadResource(ctx, req.ID, req.Params)
	case "resources/templates/list", "resources/templates":
		return s.handleListResourceTemplates(req.ID)
	case "ping":
		return rpcSuccess(req.ID, map[string]any{})
	default:
		return rpcErrorResponse(req.ID, -32601, "method not found", nil)
	}
}

func (s *Server) handleListTools(id json.RawMessage) *rpcResponse {
	tools := s.registry.SortedTools()
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		entry := map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"inputSchema":  tool.InputSchema,
			"outputSchema": tool.OutputSchema,
		}
		if tool.Annotations != nil {
			entry["annotations"] = tool.Annotations
		}
		result = append(result, entry)
	}
	return rpcSuccess(id, map[string]any{"tools": result})
}

func (s *Server) handleListResources(id json.RawMessage) *rpcResponse {
	resources := s.registry.SortedResources()
	result := make([]map[string]any, 0, len(resources))
	for _, res := range resources {
		result = append(result, map[string]any{
			"uri":         res.URI,
			"name":        res.Name,
			"description": res.Description,
			"mimeType":    res.MimeType,
		})
	}
	return rpcSuccess(id, map[string]any{"resources": result})
}

func (s *Server) handleCallTool(ctx context.Context, id json.RawMessage, params json.RawMessage) *rpcResponse {
	var payload toolCallParams
	if err := json.Unmarshal(params, &payload); err != nil {
		return rpcErrorResponse(id, -32602, "invalid params", nil)
	}
	if payload.Name == "" {
		return rpcErrorResponse(id, -32602, "missing tool name", nil)
	}
	tool, ok := s.registry.Tools[payload.Name]
	if !ok {
		return rpcErrorResponse(id, -32601, "unknown tool", nil)
	}
	args := payload.Arguments
	if args == nil {
		args = map[string]any{}
	}
	if tool.Validator != nil {
		if err := tool.Validator.Validate(args); err != nil {
			return rpcErrorResponse(id, -32602, s.redactor.Redact(err.Error()), nil)
		}
	}

	// Extract session ID from context
	sessionID, _ := ctx.Value(SessionIDKey).(string)

	// Measure request size for audit
	reqBytes, _ := json.Marshal(args)
	reqSize := int64(len(reqBytes))

	// Fire start hook before execution (for real-time activity tracking)
	if s.toolCallStartHook != nil {
		s.toolCallStartHook(ctx, ToolCallStartEvent{
			SessionID: sessionID,
			ToolName:  payload.Name,
			APIName:   tool.Operation.ServiceName,
		})
	}

	startTime := time.Now()
	result, err := s.executor.Execute(ctx, tool.Operation, args)
	duration := time.Since(startTime)

	if err != nil {
		if s.toolCallHook != nil {
			s.toolCallHook(ctx, ToolCallEvent{
				SessionID:   sessionID,
				ToolName:    payload.Name,
				APIName:     tool.Operation.ServiceName,
				Arguments:   args,
				Duration:    duration,
				Success:     false,
				ErrorMsg:    err.Error(),
				RequestSize: reqSize,
			})
		}
		return rpcErrorResponse(id, -32000, s.redactor.Redact(err.Error()), nil)
	}

	// Apply response truncation — per-API limit takes precedence over default
	maxBytes := s.maxResponseBytes
	if apiLimit, ok := s.maxResponseByAPI[tool.Operation.ServiceName]; ok {
		maxBytes = apiLimit
	}
	if maxBytes > 0 {
		result = runtime.TruncateResult(result, maxBytes)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return rpcErrorResponse(id, -32000, "failed to encode tool response", nil)
	}

	if s.toolCallHook != nil {
		s.toolCallHook(ctx, ToolCallEvent{
			SessionID:    sessionID,
			ToolName:     payload.Name,
			APIName:      tool.Operation.ServiceName,
			Arguments:    args,
			Duration:     duration,
			Success:      true,
			RequestSize:  reqSize,
			ResponseSize: int64(len(encoded)),
		})
	}

	return rpcSuccess(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(encoded)}},
		"isError": false,
	})
}

func (s *Server) handleListResourceTemplates(id json.RawMessage) *rpcResponse {
	templates := s.registry.BuildResourceTemplates()
	return rpcSuccess(id, map[string]any{"resourceTemplates": templates})
}

func (s *Server) handleReadResource(ctx context.Context, id json.RawMessage, params json.RawMessage) *rpcResponse {
	var payload resourceReadParams
	if err := json.Unmarshal(params, &payload); err != nil {
		return rpcErrorResponse(id, -32602, "invalid params", nil)
	}
	if payload.URI == "" {
		return rpcErrorResponse(id, -32602, "missing uri", nil)
	}
	res, ok := s.registry.Resources[payload.URI]
	if !ok {
		return rpcErrorResponse(id, -32601, "unknown resource", nil)
	}
	tool, ok := s.registry.Tools[res.ToolName]
	if !ok {
		return rpcErrorResponse(id, -32601, "unknown tool", nil)
	}
	args := payload.Arguments
	if args == nil {
		args = map[string]any{}
	}
	if tool.Validator != nil {
		if err := tool.Validator.Validate(args); err != nil {
			return rpcErrorResponse(id, -32602, s.redactor.Redact(err.Error()), nil)
		}
	}
	result, err := s.executor.Execute(ctx, tool.Operation, args)
	if err != nil {
		return rpcErrorResponse(id, -32000, s.redactor.Redact(err.Error()), nil)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return rpcErrorResponse(id, -32000, "failed to encode resource", nil)
	}
	return rpcSuccess(id, map[string]any{
		"contents": []map[string]any{
			{
				"uri":      payload.URI,
				"mimeType": "application/json",
				"text":     string(encoded),
			},
		},
	})
}

// RPC types

// RPCRequest represents an MCP JSON-RPC request
type RPCRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// RPCResponse represents an MCP JSON-RPC response
type RPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents an MCP JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Internal types (backwards compatibility)
type rpcRequest = RPCRequest
type rpcResponse = RPCResponse
type rpcError = RPCError

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type resourceReadParams struct {
	URI       string         `json:"uri"`
	Arguments map[string]any `json:"arguments"`
}

func rpcSuccess(id json.RawMessage, result any) *rpcResponse {
	return &rpcResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Result:  result,
	}
}

func rpcErrorResponse(id json.RawMessage, code int, message string, data any) *rpcResponse {
	return &rpcResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}
