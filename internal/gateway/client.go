package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Client communicates with config server gateway
type Client struct {
	baseURL      string
	profileName  string
	profileToken string
	httpClient   *http.Client
	logger       *log.Logger
	debug        bool

	// WebSocket fields
	wsConn        *websocket.Conn
	wsConnMu      sync.Mutex
	pendingCalls  map[any]chan *jsonrpcResponse
	pendingMu     sync.Mutex
	nextID        atomic.Int64
	notifyHandler func(method string, params json.RawMessage)
}

// jsonrpcResponse represents a JSON-RPC response
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError represents a JSON-RPC error
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// jsonrpcRequest represents a JSON-RPC request
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// NewClient creates a new gateway client (HTTP-based by default)
func NewClient(baseURL, profileName, profileToken string) *Client {
	return &Client{
		baseURL:      baseURL,
		profileName:  profileName,
		profileToken: profileToken,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		pendingCalls: make(map[any]chan *jsonrpcResponse),
		logger:       log.New(io.Discard, "", 0),
	}
}

// SetLogger configures logging for the client. When debug is true, verbose
// protocol-level messages are emitted; otherwise the logger is only used
// for errors.
func (c *Client) SetLogger(logger *log.Logger, debug bool) {
	c.logger = logger
	c.debug = debug
}

func (c *Client) debugf(format string, args ...any) {
	if c.debug {
		c.logger.Printf("[GATEWAY CLIENT] "+format, args...)
	}
}

// ConnectWebSocket establishes a WebSocket connection to the gateway
func (c *Client) ConnectWebSocket(ctx context.Context) error {
	c.wsConnMu.Lock()
	defer c.wsConnMu.Unlock()

	if c.wsConn != nil {
		return nil // Already connected
	}

	// Convert http:// to ws:// and https:// to wss://
	wsURL, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parse base URL: %w", err)
	}
	if wsURL.Scheme == "http" {
		wsURL.Scheme = "ws"
	} else if wsURL.Scheme == "https" {
		wsURL.Scheme = "wss"
	}
	wsURL.Path = fmt.Sprintf("/profiles/%s/gateway", c.profileName)

	// Create WebSocket connection with authorization header
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.profileToken)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL.String(), header)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	c.wsConn = conn

	// Start message handler goroutine
	go c.handleWebSocketMessages()

	c.debugf("WebSocket connected and message handler started")
	return nil
}

// handleWebSocketMessages processes incoming WebSocket messages
func (c *Client) handleWebSocketMessages() {
	c.debugf("Message handler goroutine started")
	for {
		c.wsConnMu.Lock()
		conn := c.wsConn
		c.wsConnMu.Unlock()

		if conn == nil {
			c.debugf("Connection is nil, exiting message handler")
			break
		}

		c.debugf("Waiting for WebSocket message...")
		var msg jsonrpcResponse
		err := conn.ReadJSON(&msg)
		if err != nil {
			c.debugf("ReadJSON error: %v", err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.debugf("Unexpected close error")
			}
			c.wsConnMu.Lock()
			c.wsConn = nil
			c.wsConnMu.Unlock()
			c.debugf("Connection closed, exiting message handler")
			break
		}
		c.debugf("Received message with ID=%v", msg.ID)

		// Check if this is a response to a pending call
		if msg.ID != nil {
			// JSON unmarshals numbers as float64, but we store IDs as int64
			// Convert float64 to int64 for map lookup
			var lookupID any = msg.ID
			if f, ok := msg.ID.(float64); ok {
				lookupID = int64(f)
			}

			c.pendingMu.Lock()
			if ch, ok := c.pendingCalls[lookupID]; ok {
				ch <- &msg
				close(ch)
				delete(c.pendingCalls, lookupID)
			}
			c.pendingMu.Unlock()
		} else if c.notifyHandler != nil {
			// This is a notification (no ID)
			// Extract method from the message (we need to read it as a different type)
			c.notifyHandler("", msg.Result)
		}
	}
}

// CloseWebSocket closes the WebSocket connection
func (c *Client) CloseWebSocket() error {
	c.wsConnMu.Lock()
	defer c.wsConnMu.Unlock()

	if c.wsConn != nil {
		err := c.wsConn.Close()
		c.wsConn = nil
		return err
	}
	return nil
}

// SetNotificationHandler sets a handler for server-initiated notifications
func (c *Client) SetNotificationHandler(handler func(method string, params json.RawMessage)) {
	c.notifyHandler = handler
}

// ExecuteWebSocket executes a tool via WebSocket
func (c *Client) ExecuteWebSocket(ctx context.Context, toolName string, arguments map[string]any) (*Result, error) {
	c.debugf("ExecuteWebSocket called for tool=%s", toolName)
	// Ensure WebSocket is connected
	c.wsConnMu.Lock()
	conn := c.wsConn
	c.wsConnMu.Unlock()

	if conn == nil {
		c.debugf("ExecuteWebSocket: websocket not connected!")
		return nil, fmt.Errorf("websocket not connected")
	}
	c.debugf("ExecuteWebSocket: WebSocket is connected")

	// Generate request ID
	id := c.nextID.Add(1)

	// Create JSON-RPC request
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "execute",
		Params: map[string]any{
			"tool_name": toolName,
			"arguments": arguments,
		},
	}

	// Create response channel
	respCh := make(chan *jsonrpcResponse, 1)
	c.pendingMu.Lock()
	c.pendingCalls[id] = respCh
	c.pendingMu.Unlock()

	// Send request
	c.wsConnMu.Lock()
	err := conn.WriteJSON(req)
	c.wsConnMu.Unlock()
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pendingCalls, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("jsonrpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		// Parse result
		var result Result
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("parse result: %w", err)
		}
		return &result, nil

	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pendingCalls, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// FetchToolsWebSocket fetches tools via WebSocket
func (c *Client) FetchToolsWebSocket(ctx context.Context) ([]Tool, error) {
	c.debugf("FetchToolsWebSocket called")
	// Ensure WebSocket is connected
	c.wsConnMu.Lock()
	conn := c.wsConn
	c.wsConnMu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("websocket not connected")
	}

	// Generate request ID
	id := c.nextID.Add(1)
	c.debugf("Generated request ID=%d for tools/list", id)

	// Create JSON-RPC request
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/list",
	}

	// Create response channel
	respCh := make(chan *jsonrpcResponse, 1)
	c.pendingMu.Lock()
	c.pendingCalls[id] = respCh
	c.pendingMu.Unlock()

	// Send request
	c.wsConnMu.Lock()
	err := conn.WriteJSON(req)
	c.wsConnMu.Unlock()
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pendingCalls, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("write request: %w", err)
	}
	c.debugf("Sent tools/list request, waiting for response...")

	// Wait for response
	select {
	case resp := <-respCh:
		c.debugf("Received tools/list response")
		if resp.Error != nil {
			return nil, fmt.Errorf("jsonrpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		// Parse result
		var result struct {
			Tools []Tool `json:"tools"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("parse result: %w", err)
		}
		c.debugf("FetchToolsWebSocket returning %d tools (WebSocket should stay open)", len(result.Tools))
		return result.Tools, nil

	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pendingCalls, id)
		c.pendingMu.Unlock()
		c.debugf("FetchToolsWebSocket context cancelled")
		return nil, ctx.Err()
	}
}

// FetchTools retrieves available tools from gateway
func (c *Client) FetchTools(ctx context.Context) ([]Tool, error) {
	url := fmt.Sprintf("%s/profiles/%s/tools", c.baseURL, c.profileName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.profileToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gateway returned %d", resp.StatusCode)
	}

	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Tools, nil
}

// Execute sends an operation to the gateway for execution
func (c *Client) Execute(ctx context.Context, toolName string, arguments map[string]any) (*Result, error) {
	url := fmt.Sprintf("%s/profiles/%s/execute", c.baseURL, c.profileName)

	reqBody := map[string]any{
		"tool_name": toolName,
		"arguments": arguments,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.profileToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gateway returned %d", resp.StatusCode)
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Tool represents a tool definition from the gateway
type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`
}

// Result represents the execution result from the gateway
type Result struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        any    `json:"body"`
}
