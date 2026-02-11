package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"

	"skyline-mcp/internal/canonical"
	"skyline-mcp/internal/redact"
	"skyline-mcp/internal/runtime"
)

const protocolVersion = "2025-11-25"

// Executor interface for executing operations
type Executor interface {
	Execute(ctx context.Context, op *canonical.Operation, args map[string]any) (*runtime.Result, error)
}

type Server struct {
	registry     *Registry
	executor     Executor    // Runtime executor for tool calls
	codeExecutor interface{} // Code executor for /execute endpoint (optional)
	logger       *log.Logger
	redactor     *redact.Redactor
}

func NewServer(registry *Registry, executor Executor, logger *log.Logger, redactor *redact.Redactor) *Server {
	return &Server{
		registry: registry,
		executor: executor,
		logger:   logger,
		redactor: redactor,
	}
}

// SetCodeExecutor sets the code executor for /execute endpoint
func (s *Server) SetCodeExecutor(exec interface{}) {
	s.codeExecutor = exec
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
				"version": "0.1.0",
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
	case "resources/templates":
		return rpcSuccess(req.ID, map[string]any{"templates": []any{}})
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
		result = append(result, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
			"outputSchema": tool.OutputSchema,
		})
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
	result, err := s.executor.Execute(ctx, tool.Operation, args)
	if err != nil {
		return rpcErrorResponse(id, -32000, s.redactor.Redact(err.Error()), nil)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return rpcErrorResponse(id, -32000, "failed to encode tool response", nil)
	}
	return rpcSuccess(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(encoded)}},
		"isError": false,
	})
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

type rpcRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int   `json:"code"`
	Message string `json:"message"`
	Data    any   `json:"data,omitempty"`
}

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
