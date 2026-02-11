package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"skyline-mcp/internal/config"
)

// StreamableHTTPServer implements MCP Streamable HTTP transport (spec 2025-11-25)
// Single /mcp endpoint for both POST (requests) and GET (notifications/subscriptions)
type StreamableHTTPServer struct {
	server *Server
	logger *log.Logger
	auth   *config.AuthConfig
	store  *streamableSessionStore
}

// streamableSession represents an active MCP session with event history for resumability
type streamableSession struct {
	id           string
	ch           chan *sseEvent
	createdAt    time.Time
	lastUsed     time.Time
	eventCounter uint64
	events       []*sseEvent // Ring buffer for resumability
	maxEvents    int
	mu           sync.RWMutex
}

// sseEvent represents a single SSE event with ID for resumability
type sseEvent struct {
	id   string
	name string
	data []byte
}

type streamableSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*streamableSession
}

func newStreamableSessionStore() *streamableSessionStore {
	return &streamableSessionStore{
		sessions: make(map[string]*streamableSession),
	}
}

func (s *streamableSessionStore) create(id string) *streamableSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess := &streamableSession{
		id:        id,
		ch:        make(chan *sseEvent, 128),
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		maxEvents: 100, // Keep last 100 events for resumability
		events:    make([]*sseEvent, 0, 100),
	}
	s.sessions[id] = sess
	return sess
}

func (s *streamableSessionStore) get(id string) *streamableSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess := s.sessions[id]
	if sess != nil {
		sess.lastUsed = time.Now()
	}
	return sess
}

func (s *streamableSessionStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		close(sess.ch)
		delete(s.sessions, id)
	}
}

func (s *streamableSessionStore) cleanup(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, sess := range s.sessions {
		if now.Sub(sess.lastUsed) > maxAge {
			close(sess.ch)
			delete(s.sessions, id)
		}
	}
}

func (sess *streamableSession) addEvent(event *sseEvent) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Add to ring buffer (keep last N events)
	sess.events = append(sess.events, event)
	if len(sess.events) > sess.maxEvents {
		sess.events = sess.events[1:]
	}

	// Send to active stream (non-blocking)
	select {
	case sess.ch <- event:
	default:
		// Channel full, log but don't block
	}
}

func (sess *streamableSession) replayFrom(lastEventID string) []*sseEvent {
	sess.mu.RLock()
	defer sess.mu.RUnlock()

	if lastEventID == "" {
		return nil
	}

	// Find events after lastEventID
	var replay []*sseEvent
	found := false
	for _, evt := range sess.events {
		if found {
			replay = append(replay, evt)
		} else if evt.id == lastEventID {
			found = true
		}
	}
	return replay
}

func NewStreamableHTTPServer(server *Server, logger *log.Logger, auth *config.AuthConfig) *StreamableHTTPServer {
	s := &StreamableHTTPServer{
		server: server,
		logger: logger,
		auth:   auth,
		store:  newStreamableSessionStore(),
	}

	// Start cleanup goroutine
	go s.cleanupLoop()

	return s
}

func (h *StreamableHTTPServer) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.store.cleanup(1 * time.Hour) // Remove sessions inactive for 1 hour
	}
}

func (h *StreamableHTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", h.handleMCP)
	mux.HandleFunc("/execute", h.server.HandleExecute)
	mux.HandleFunc("/internal/call-tool", h.server.HandleInternalToolCall)
	mux.HandleFunc("/internal/search-tools", h.server.HandleSearchTools)
	mux.HandleFunc("/agent-prompt", h.server.HandleAgentPrompt)
	return mux
}

func (h *StreamableHTTPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers on all responses (not just OPTIONS)
	origin := r.Header.Get("Origin")
	if origin != "" {
		// Allow all origins when origin header is present
		// Security is enforced via bearer token authentication
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
	}

	// Route based on method
	switch r.Method {
	case http.MethodGet:
		h.handleGET(w, r)
	case http.MethodPost:
		h.handlePOST(w, r)
	case http.MethodDelete:
		h.handleDELETE(w, r)
	case http.MethodOptions:
		h.handleOPTIONS(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGET implements GET /mcp for server notifications and subscriptions
// This opens an SSE stream that the server can use to send notifications
func (h *StreamableHTTPServer) handleGET(w http.ResponseWriter, r *http.Request) {
	// Security check: bearer token authentication
	if !authorizeRequest(r, h.auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Note: Origin validation removed - CORS handled at top level, security via bearer token
	if !hasAccept(r.Header, "text/event-stream") {
		http.Error(w, "missing accept: text/event-stream", http.StatusBadRequest)
		return
	}

	// Get session ID from header
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "missing Mcp-Session-Id header", http.StatusBadRequest)
		return
	}

	// Get or create session
	sess := h.store.get(sessionID)
	if sess == nil {
		// Session doesn't exist - client should initialize first
		http.Error(w, "session not found - initialize first", http.StatusNotFound)
		return
	}

	// Check for SSE support
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Check for resumability
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID != "" {
		h.logger.Printf("[streamable] resuming stream from event %s", lastEventID)
		// Replay missed events
		replayEvents := sess.replayFrom(lastEventID)
		for _, evt := range replayEvents {
			if err := h.writeSSEWithID(w, evt.name, evt.data, evt.id); err != nil {
				return
			}
			flusher.Flush()
		}
	}

	// Send heartbeat pings and notifications
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	h.logger.Printf("[streamable] opened GET stream for session %s", sessionID)

	for {
		select {
		case <-r.Context().Done():
			h.logger.Printf("[streamable] GET stream closed for session %s", sessionID)
			return

		case <-ticker.C:
			// Send heartbeat comment (no event, just keeps connection alive)
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()

		case event := <-sess.ch:
			// Send notification/request from server
			if err := h.writeSSEWithID(w, event.name, event.data, event.id); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handlePOST implements POST /mcp for client requests
// Can return either JSON (quick response) or SSE stream (long-running operations)
func (h *StreamableHTTPServer) handlePOST(w http.ResponseWriter, r *http.Request) {
	// Security check: bearer token authentication
	if !authorizeRequest(r, h.auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Note: Origin validation removed - CORS handled at top level, security via bearer token
	if !hasAccept(r.Header, "application/json") && !hasAccept(r.Header, "text/event-stream") {
		http.Error(w, "missing accept header", http.StatusBadRequest)
		return
	}
	if !validateProtocolHeader(r.Header) {
		http.Error(w, "unsupported protocol version", http.StatusBadRequest)
		return
	}

	// Read request body
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Handle batch requests
	if body[0] == '[' {
		var batch []rpcRequest
		if err := json.Unmarshal(body, &batch); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		var responses []*rpcResponse
		for i := range batch {
			resp := h.server.handleRequest(ctx, &batch[i])
			if resp != nil {
				responses = append(responses, resp)
			}
		}

		if len(responses) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(responses); err != nil {
			h.logger.Printf("[streamable] encode error: %v", err)
		}
		return
	}

	// Handle single request
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Special handling for initialize - create session and return session ID
	if req.Method == "initialize" {
		sessionID := newSessionID()
		sess := h.store.create(sessionID)

		h.logger.Printf("[streamable] created session %s", sessionID)

		resp := h.server.handleRequest(ctx, &req)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// Return session ID in header
		w.Header().Set("Mcp-Session-Id", sessionID)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			h.logger.Printf("[streamable] encode error: %v", err)
		}

		// Start sending initial notifications on the session channel
		go h.sendInitialNotifications(sess)
		return
	}

	// For other requests, handle normally
	resp := h.server.handleRequest(ctx, &req)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Check if client accepts streaming
	if hasAccept(r.Header, "text/event-stream") {
		// For now, always return JSON (streaming for long operations can be added later)
		// To implement: check if operation is long-running, then stream incremental updates
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			h.logger.Printf("[streamable] encode error: %v", err)
		}
		return
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Printf("[streamable] encode error: %v", err)
	}
}

// handleDELETE implements DELETE /mcp for explicit session termination
func (h *StreamableHTTPServer) handleDELETE(w http.ResponseWriter, r *http.Request) {
	if !authorizeRequest(r, h.auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "missing Mcp-Session-Id header", http.StatusBadRequest)
		return
	}

	h.store.remove(sessionID)
	h.logger.Printf("[streamable] session %s terminated by client", sessionID)

	w.WriteHeader(http.StatusNoContent)
}

// handleOPTIONS implements CORS preflight for /mcp
func (h *StreamableHTTPServer) handleOPTIONS(w http.ResponseWriter, r *http.Request) {
	// CORS headers already set in handleMCP, just add method-specific headers
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
	w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
	w.WriteHeader(http.StatusNoContent)
}

// sendInitialNotifications sends any initial server notifications after session creation
func (h *StreamableHTTPServer) sendInitialNotifications(sess *streamableSession) {
	// Example: Send tools/list_changed notification if server supports dynamic tool updates
	// This would be called whenever tools change, not just on initialization
	// For now, this is a placeholder for future notification support
}

// sendNotification sends a notification to a specific session (for server-initiated messages)
func (h *StreamableHTTPServer) sendNotification(sessionID string, method string, params interface{}) error {
	sess := h.store.get(sessionID)
	if sess == nil {
		return fmt.Errorf("session not found")
	}

	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}

	sess.eventCounter++
	eventID := fmt.Sprintf("%s-%d", sessionID, sess.eventCounter)

	event := &sseEvent{
		id:   eventID,
		name: "message",
		data: data,
	}

	sess.addEvent(event)
	return nil
}

// writeSSEWithID writes an SSE event with ID (for resumability)
func (h *StreamableHTTPServer) writeSSEWithID(w io.Writer, eventName string, data []byte, id string) error {
	bw := bufio.NewWriter(w)

	// Write event ID (for resumability)
	if id != "" {
		if _, err := fmt.Fprintf(bw, "id: %s\n", id); err != nil {
			return err
		}
	}

	// Write event name
	if eventName != "" {
		if _, err := fmt.Fprintf(bw, "event: %s\n", eventName); err != nil {
			return err
		}
	}

	// Write data (multi-line support)
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		if _, err := bw.WriteString("data: "); err != nil {
			return err
		}
		if _, err := bw.Write(line); err != nil {
			return err
		}
		if _, err := bw.WriteString("\n"); err != nil {
			return err
		}
	}

	// End event
	if _, err := bw.WriteString("\n"); err != nil {
		return err
	}

	return bw.Flush()
}

// validateOriginString validates origin without needing *http.Request
func validateOriginString(origin, requestHost string) bool {
	if origin == "" {
		return true
	}

	// Parse origin
	if !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
		return false
	}

	// Extract host from origin
	originHost := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	originHost = strings.Split(originHost, "/")[0]
	originHost = strings.Split(originHost, ":")[0]

	// Check localhost
	switch strings.ToLower(originHost) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}

	// Check same origin
	if requestHost != "" {
		reqHost := strings.Split(requestHost, ":")[0]
		if strings.EqualFold(reqHost, originHost) {
			return true
		}
	}

	return false
}

// Helper functions are reused from http_sse.go (newSessionID, authorizeRequest, validateOrigin, hasAccept, validateProtocolHeader)
