package mcp

import (
	"sync"
	"sync/atomic"
	"time"
)

// ClientInfo describes the MCP client (parsed from initialize params.clientInfo).
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ActiveSession represents a live MCP session with per-session stats.
type ActiveSession struct {
	ID            string      `json:"id"`
	Profile       string      `json:"profile"`
	ClientInfo    *ClientInfo `json:"client_info"`
	ConnectedAt   time.Time   `json:"connected_at"`
	CurrentTool   string      `json:"current_tool"`
	ToolStartedAt *time.Time  `json:"tool_started_at,omitempty"`

	requestCount atomic.Int64
	errorCount   atomic.Int64
	bytesIn      atomic.Int64
	bytesOut     atomic.Int64
	mu           sync.Mutex // protects CurrentTool, ToolStartedAt
}

// SessionSnapshot is the JSON-serializable view of an ActiveSession.
type SessionSnapshot struct {
	ID            string      `json:"id"`
	Profile       string      `json:"profile"`
	ClientInfo    *ClientInfo `json:"client_info"`
	ConnectedAt   time.Time   `json:"connected_at"`
	CurrentTool   string      `json:"current_tool"`
	ToolStartedAt *time.Time  `json:"tool_started_at,omitempty"`
	RequestCount  int64       `json:"request_count"`
	ErrorCount    int64       `json:"error_count"`
	BytesIn       int64       `json:"bytes_in"`
	BytesOut      int64       `json:"bytes_out"`
}

func (s *ActiveSession) snapshot() SessionSnapshot {
	s.mu.Lock()
	currentTool := s.CurrentTool
	toolStartedAt := s.ToolStartedAt
	s.mu.Unlock()

	return SessionSnapshot{
		ID:            s.ID,
		Profile:       s.Profile,
		ClientInfo:    s.ClientInfo,
		ConnectedAt:   s.ConnectedAt,
		CurrentTool:   currentTool,
		ToolStartedAt: toolStartedAt,
		RequestCount:  s.requestCount.Load(),
		ErrorCount:    s.errorCount.Load(),
		BytesIn:       s.bytesIn.Load(),
		BytesOut:      s.bytesOut.Load(),
	}
}

// SessionTracker is a global registry of active MCP sessions.
type SessionTracker struct {
	mu       sync.RWMutex
	sessions map[string]*ActiveSession
}

// NewSessionTracker creates a new session tracker.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]*ActiveSession),
	}
}

// Register adds a new active session.
func (t *SessionTracker) Register(id, profile string, clientInfo *ClientInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions[id] = &ActiveSession{
		ID:          id,
		Profile:     profile,
		ClientInfo:  clientInfo,
		ConnectedAt: time.Now(),
	}
}

// Unregister removes a session. Returns true if it existed.
func (t *SessionTracker) Unregister(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.sessions[id]; ok {
		delete(t.sessions, id)
		return true
	}
	return false
}

// RecordToolStart marks a session as currently executing a tool.
func (t *SessionTracker) RecordToolStart(sessionID, toolName string) {
	t.mu.RLock()
	sess, ok := t.sessions[sessionID]
	t.mu.RUnlock()
	if !ok {
		return
	}
	now := time.Now()
	sess.mu.Lock()
	sess.CurrentTool = toolName
	sess.ToolStartedAt = &now
	sess.mu.Unlock()
}

// RecordToolEnd marks a session as idle and records stats.
func (t *SessionTracker) RecordToolEnd(sessionID, toolName string, success bool, reqSize, respSize int64) {
	t.mu.RLock()
	sess, ok := t.sessions[sessionID]
	t.mu.RUnlock()
	if !ok {
		return
	}
	sess.requestCount.Add(1)
	if !success {
		sess.errorCount.Add(1)
	}
	sess.bytesIn.Add(reqSize)
	sess.bytesOut.Add(respSize)

	sess.mu.Lock()
	sess.CurrentTool = ""
	sess.ToolStartedAt = nil
	sess.mu.Unlock()
}

// Snapshot returns a snapshot of all active sessions.
func (t *SessionTracker) Snapshot() []SessionSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]SessionSnapshot, 0, len(t.sessions))
	for _, sess := range t.sessions {
		result = append(result, sess.snapshot())
	}
	return result
}

// Get returns a snapshot of a single session, or nil if not found.
func (t *SessionTracker) Get(id string) *SessionSnapshot {
	t.mu.RLock()
	sess, ok := t.sessions[id]
	t.mu.RUnlock()
	if !ok {
		return nil
	}
	snap := sess.snapshot()
	return &snap
}
