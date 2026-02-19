package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"

	"net/http"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

// handleGatewayWebSocket handles WebSocket connections for bidirectional gateway communication
func (s *server) handleGatewayWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract profile name from URL path
	name := extractProfileName(r.URL.Path, "/profiles/", "/gateway")
	if name == "" {
		http.Error(w, "profile name required", http.StatusBadRequest)
		return
	}

	// Authenticate request (check token before upgrading)
	s.mu.RLock()
	prof, ok := s.findProfile(name)
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.authorizeProfile(r, prof); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Printf("websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	s.logger.Printf("websocket gateway connected: profile=%s", name)

	// Log connection
	clientAddr := conn.RemoteAddr().String()
	s.auditLogger.LogConnect(name, clientAddr)
	s.metrics.RecordConnection(true)

	// Create gateway session
	session := &gatewaySession{
		server:        s,
		conn:          conn,
		profile:       prof,
		logger:        s.logger,
		subscriptions: make(map[string]context.CancelFunc),
	}

	// Handle messages
	session.handleMessages()

	// Log disconnection
	s.auditLogger.LogDisconnect(name, clientAddr)
	s.metrics.RecordConnection(false)
}

// handleMessages processes incoming WebSocket messages
func (gs *gatewaySession) handleMessages() {
	gs.logger.Printf("[WS] Starting message handler loop for profile=%s", gs.profile.Name)
	for {
		gs.logger.Printf("[WS] Waiting for next message...")
		var msg jsonrpcMessage
		err := gs.conn.ReadJSON(&msg)
		if err != nil {
			gs.logger.Printf("[WS] ReadJSON error: %v (unexpected=%v)", err, websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure))
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				gs.logger.Printf("websocket error: %v", err)
			}
			break
		}

		gs.logger.Printf("[WS] Received message: method=%s, id=%v", msg.Method, msg.ID)
		// Route message based on method
		gs.routeMessage(&msg)
		gs.logger.Printf("[WS] Message handled, continuing loop...")
	}

	// Clean up subscriptions on disconnect
	gs.mu.Lock()
	for id, cancel := range gs.subscriptions {
		cancel()
		delete(gs.subscriptions, id)
	}
	gs.mu.Unlock()

	gs.logger.Printf("websocket gateway disconnected: profile=%s", gs.profile.Name)
}

// routeMessage routes JSON-RPC messages to appropriate handlers
func (gs *gatewaySession) routeMessage(msg *jsonrpcMessage) {
	switch msg.Method {
	case "execute":
		gs.handleExecute(msg)
	case "subscribe":
		gs.handleSubscribe(msg)
	case "unsubscribe":
		gs.handleUnsubscribe(msg)
	case "tools/list":
		gs.handleToolsList(msg)
	default:
		gs.sendError(msg.ID, -32601, fmt.Sprintf("method not found: %s", msg.Method))
	}
}

// handleExecute handles tool execution requests
func (gs *gatewaySession) handleExecute(msg *jsonrpcMessage) {
	startTime := time.Now()
	var params struct {
		ToolName  string         `json:"tool_name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		gs.sendError(msg.ID, -32602, "invalid params")
		gs.server.auditLogger.LogError(gs.profile.Name, "execute", "invalid params", gs.conn.RemoteAddr().String())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cached, _, err := gs.server.getOrBuildCache(ctx, gs.profile)
	if err != nil {
		errMsg := fmt.Sprintf("load services: %v", err)
		gs.sendError(msg.ID, -32603, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, "", params.ToolName, params.Arguments,
			time.Since(startTime), 0, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, time.Since(startTime), false)
		return
	}

	// Look up the tool by name
	tool, ok := cached.registry.Tools[params.ToolName]
	if !ok {
		errMsg := fmt.Sprintf("unknown tool: %s", params.ToolName)
		gs.sendError(msg.ID, -32602, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, "", params.ToolName, params.Arguments,
			time.Since(startTime), 404, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, time.Since(startTime), false)
		return
	}

	// Execute the operation
	result, err := cached.executor.Execute(ctx, tool.Operation, params.Arguments)
	duration := time.Since(startTime)

	if err != nil {
		errMsg := fmt.Sprintf("execute: %v", err)
		gs.sendError(msg.ID, -32603, errMsg)
		gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, tool.Operation.ServiceName, params.ToolName, params.Arguments,
			duration, 0, false, errMsg, gs.conn.RemoteAddr().String())
		gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, duration, false)
		return
	}

	// Log successful execution
	gs.server.auditLogger.LogExecute(ctx, gs.profile.Name, tool.Operation.ServiceName, params.ToolName, params.Arguments,
		duration, result.Status, true, "", gs.conn.RemoteAddr().String())
	gs.server.metrics.RecordRequest(gs.profile.Name, params.ToolName, duration, true)

	// Send success response
	gs.sendResult(msg.ID, result)
}

// handleToolsList handles tools/list requests
func (gs *gatewaySession) handleToolsList(msg *jsonrpcMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cached, _, err := gs.server.getOrBuildCache(ctx, gs.profile)
	if err != nil {
		gs.sendError(msg.ID, -32603, fmt.Sprintf("load services: %v", err))
		return
	}

	// Convert registry tools to response format
	tools := make([]toolInfo, 0, len(cached.registry.Tools))
	for _, tool := range cached.registry.Tools {
		tools = append(tools, toolInfo{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
		})
	}

	gs.sendResult(msg.ID, map[string]any{"tools": tools})
}

// handleSubscribe handles subscription requests (placeholder for future implementation)
func (gs *gatewaySession) handleSubscribe(msg *jsonrpcMessage) {
	var params struct {
		Resource string         `json:"resource"`
		Params   map[string]any `json:"params"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		gs.sendError(msg.ID, -32602, "invalid params")
		return
	}

	// For now, just acknowledge the subscription
	// Future: implement actual streaming/subscription logic
	gs.sendResult(msg.ID, map[string]any{
		"subscription_id": fmt.Sprintf("sub_%v", msg.ID),
		"status":          "subscribed",
	})
}

// handleUnsubscribe handles unsubscribe requests
func (gs *gatewaySession) handleUnsubscribe(msg *jsonrpcMessage) {
	var params struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		gs.sendError(msg.ID, -32602, "invalid params")
		return
	}

	gs.mu.Lock()
	if cancel, ok := gs.subscriptions[params.SubscriptionID]; ok {
		cancel()
		delete(gs.subscriptions, params.SubscriptionID)
	}
	gs.mu.Unlock()

	gs.sendResult(msg.ID, map[string]any{"status": "unsubscribed"})
}

// sendResult sends a JSON-RPC success response
func (gs *gatewaySession) sendResult(id any, result any) {
	response := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	if err := gs.conn.WriteJSON(response); err != nil {
		gs.logger.Printf("websocket write error: %v", err)
	}
}

// sendError sends a JSON-RPC error response
func (gs *gatewaySession) sendError(id any, code int, message string) {
	response := jsonrpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonrpcError{
			Code:    code,
			Message: message,
		},
	}
	if err := gs.conn.WriteJSON(response); err != nil {
		gs.logger.Printf("websocket write error: %v", err)
	}
}

// sendNotification sends a JSON-RPC notification (no ID, no response expected)
func (gs *gatewaySession) sendNotification(method string, params any) {
	notification := jsonrpcMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  mustMarshal(params),
	}
	if err := gs.conn.WriteJSON(notification); err != nil {
		gs.logger.Printf("websocket write error: %v", err)
	}
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
