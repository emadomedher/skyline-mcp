package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"skyline-mcp/internal/config"
)

type HTTPServer struct {
	server *Server
	logger *slog.Logger
	auth   *config.AuthConfig
	store  *sessionStore
}

func NewHTTPServer(server *Server, logger *slog.Logger, auth *config.AuthConfig) *HTTPServer {
	return &HTTPServer{
		server: server,
		logger: logger,
		auth:   auth,
		store:  newSessionStore(),
	}
}

func (h *HTTPServer) Serve(ctx context.Context, addr string) error {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: h.handler(),
	}
	go func() {
		<-ctx.Done()
		_ = httpServer.Shutdown(context.Background())
	}()
	return httpServer.ListenAndServe()
}

func (h *HTTPServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", h.handleSSE)
	mux.HandleFunc("/message", h.handleMessage)
	mux.HandleFunc("/mcp", h.handleStreamableHTTP)
	mux.HandleFunc("/execute", h.server.HandleExecute)
	mux.HandleFunc("/internal/call-tool", h.server.HandleInternalToolCall)
	mux.HandleFunc("/internal/search-tools", h.server.HandleSearchTools)
	mux.HandleFunc("/agent-prompt", h.server.HandleAgentPrompt)
	return mux
}

func (h *HTTPServer) handleStreamableHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.handleStreamableHTTPGet(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.handleStreamableHTTPPost(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h *HTTPServer) handleStreamableHTTPGet(w http.ResponseWriter, r *http.Request) {
	if !authorizeRequest(r, h.auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !validateOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !hasAccept(r.Header, "text/event-stream") {
		http.Error(w, "missing accept", http.StatusBadRequest)
		return
	}
	http.Error(w, "streaming not implemented", http.StatusMethodNotAllowed)
}

func (h *HTTPServer) handleStreamableHTTPPost(w http.ResponseWriter, r *http.Request) {
	if !authorizeRequest(r, h.auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !validateOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !hasAccept(r.Header, "application/json") || !hasAccept(r.Header, "text/event-stream") {
		http.Error(w, "missing accept", http.StatusBadRequest)
		return
	}
	if !validateProtocolHeader(r.Header) {
		http.Error(w, "unsupported mcp protocol version", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10MB limit
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
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
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	resp := h.server.handleRequest(ctx, &req)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
	}
}

func (h *HTTPServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeRequest(r, h.auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := newSessionID()
	ch := make(chan []byte, 128)
	h.store.add(sessionID, ch)
	defer h.store.remove(sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	endpoint := buildMessageURL(r, sessionID)
	endpointPayload, _ := json.Marshal(map[string]string{"url": endpoint})
	_ = writeSSE(w, "endpoint", endpointPayload)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case msg := <-ch:
			if err := writeSSE(w, "message", msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *HTTPServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeRequest(r, h.auth) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = r.Header.Get("Mcp-Session-Id")
	}
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	ch := h.store.get(sessionID)
	if ch == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10MB limit
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if body[0] == '[' {
		var batch []rpcRequest
		if err := json.Unmarshal(body, &batch); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		for i := range batch {
			h.dispatch(ctx, ch, &batch[i])
		}
	} else {
		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		h.dispatch(ctx, ch, &req)
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *HTTPServer) dispatch(ctx context.Context, ch chan []byte, req *rpcRequest) {
	resp := h.server.handleRequest(ctx, req)
	if resp == nil {
		return
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		h.logger.Error("sse encode error", "error", err)
		return
	}
	select {
	case ch <- encoded:
	default:
		h.logger.Warn("sse buffer full, dropping message")
	}
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]chan []byte
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: map[string]chan []byte{}}
}

func (s *sessionStore) add(id string, ch chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = ch
}

func (s *sessionStore) get(id string) chan []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

func (s *sessionStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func writeSSE(w io.Writer, event string, data []byte) error {
	bw := bufio.NewWriter(w)
	if event != "" {
		if _, err := fmt.Fprintf(bw, "event: %s\n", event); err != nil {
			return err
		}
	}
	if len(data) > 0 {
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
	} else {
		if _, err := bw.WriteString("data: \n"); err != nil {
			return err
		}
	}
	if _, err := bw.WriteString("\n"); err != nil {
		return err
	}
	return bw.Flush()
}

func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func buildMessageURL(r *http.Request, sessionID string) string {
	scheme := "http"
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	escaped := url.QueryEscape(sessionID)
	return fmt.Sprintf("%s://%s/message?session_id=%s", scheme, host, escaped)
}

func authorizeRequest(r *http.Request, auth *config.AuthConfig) bool {
	if auth == nil {
		return true
	}
	switch auth.Type {
	case "bearer":
		token := strings.TrimSpace(auth.Token)
		if token == "" {
			return false
		}
		expected := []byte("Bearer " + token)
		actual := []byte(r.Header.Get("Authorization"))
		return subtle.ConstantTimeCompare(actual, expected) == 1
	case "basic":
		if auth.Username == "" || auth.Password == "" {
			return false
		}
		expected := []byte("Basic " + base64.StdEncoding.EncodeToString([]byte(auth.Username+":"+auth.Password)))
		actual := []byte(r.Header.Get("Authorization"))
		return subtle.ConstantTimeCompare(actual, expected) == 1
	case "api-key":
		if auth.Header == "" || auth.Value == "" {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(r.Header.Get(auth.Header)), []byte(auth.Value)) == 1
	default:
		return false
	}
}

func validateOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	if isLocalHost(host) {
		return true
	}
	reqHost := r.Host
	if reqHost != "" {
		reqHost = strings.Split(reqHost, ":")[0]
		if strings.EqualFold(reqHost, host) {
			return true
		}
	}
	return false
}

func isLocalHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func hasAccept(header http.Header, value string) bool {
	accept := header.Get("Accept")
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		if strings.Contains(strings.ToLower(strings.TrimSpace(part)), strings.ToLower(value)) {
			return true
		}
	}
	return false
}

func validateProtocolHeader(header http.Header) bool {
	version := strings.TrimSpace(header.Get("Mcp-Protocol-Version"))
	if version == "" {
		return true
	}
	switch version {
	case "2025-03-26", "2025-06-18", "2025-11-25":
		return true
	default:
		return false
	}
}
