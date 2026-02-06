package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Event represents an audit log entry
type Event struct {
	ID          int64                  `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Profile     string                 `json:"profile"`
	EventType   string                 `json:"event_type"` // "execute", "connect", "disconnect", "error"
	ToolName    string                 `json:"tool_name,omitempty"`
	Arguments   map[string]interface{} `json:"arguments,omitempty"`
	DurationMs  int64                  `json:"duration_ms,omitempty"`
	StatusCode  int                    `json:"status_code,omitempty"`
	Success     bool                   `json:"success"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	ClientAddr  string                 `json:"client_addr,omitempty"`
	RequestSize int64                  `json:"request_size,omitempty"`
	ResponseSize int64                 `json:"response_size,omitempty"`
}

// Logger handles audit logging to SQLite
type Logger struct {
	db          *sql.DB
	mu          sync.Mutex
	batchSize   int
	flushTicker *time.Ticker
	buffer      []Event
	bufferMu    sync.Mutex
}

// NewLogger creates a new audit logger
func NewLogger(dbPath string) (*Logger, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Create audit_events table
	schema := `
	CREATE TABLE IF NOT EXISTS audit_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		profile TEXT NOT NULL,
		event_type TEXT NOT NULL,
		tool_name TEXT,
		arguments TEXT,
		duration_ms INTEGER,
		status_code INTEGER,
		success BOOLEAN NOT NULL,
		error_msg TEXT,
		client_addr TEXT,
		request_size INTEGER,
		response_size INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_events(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_audit_profile ON audit_events(profile);
	CREATE INDEX IF NOT EXISTS idx_audit_event_type ON audit_events(event_type);
	CREATE INDEX IF NOT EXISTS idx_audit_tool_name ON audit_events(tool_name);
	`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	logger := &Logger{
		db:        db,
		batchSize: 100,
		buffer:    make([]Event, 0, 100),
	}

	// Start background flusher (every 5 seconds)
	logger.flushTicker = time.NewTicker(5 * time.Second)
	go logger.backgroundFlush()

	return logger, nil
}

// LogExecute logs a tool execution event
func (l *Logger) LogExecute(ctx context.Context, profile, toolName string, args map[string]interface{}, duration time.Duration, statusCode int, success bool, errMsg, clientAddr string) {
	event := Event{
		Timestamp:  time.Now(),
		Profile:    profile,
		EventType:  "execute",
		ToolName:   toolName,
		Arguments:  args,
		DurationMs: duration.Milliseconds(),
		StatusCode: statusCode,
		Success:    success,
		ErrorMsg:   errMsg,
		ClientAddr: clientAddr,
	}

	l.bufferEvent(event)
}

// LogConnect logs a WebSocket connection event
func (l *Logger) LogConnect(profile, clientAddr string) {
	event := Event{
		Timestamp:  time.Now(),
		Profile:    profile,
		EventType:  "connect",
		Success:    true,
		ClientAddr: clientAddr,
	}

	l.bufferEvent(event)
}

// LogDisconnect logs a WebSocket disconnection event
func (l *Logger) LogDisconnect(profile, clientAddr string) {
	event := Event{
		Timestamp:  time.Now(),
		Profile:    profile,
		EventType:  "disconnect",
		Success:    true,
		ClientAddr: clientAddr,
	}

	l.bufferEvent(event)
}

// LogError logs an error event
func (l *Logger) LogError(profile, eventType, errMsg, clientAddr string) {
	event := Event{
		Timestamp:  time.Now(),
		Profile:    profile,
		EventType:  eventType,
		Success:    false,
		ErrorMsg:   errMsg,
		ClientAddr: clientAddr,
	}

	l.bufferEvent(event)
}

// bufferEvent adds an event to the buffer for batch insertion
func (l *Logger) bufferEvent(event Event) {
	l.bufferMu.Lock()
	defer l.bufferMu.Unlock()

	l.buffer = append(l.buffer, event)

	// Flush if buffer is full
	if len(l.buffer) >= l.batchSize {
		go l.Flush()
	}
}

// Flush writes all buffered events to the database
func (l *Logger) Flush() error {
	l.bufferMu.Lock()
	if len(l.buffer) == 0 {
		l.bufferMu.Unlock()
		return nil
	}

	// Copy buffer and clear it
	events := make([]Event, len(l.buffer))
	copy(events, l.buffer)
	l.buffer = l.buffer[:0]
	l.bufferMu.Unlock()

	// Insert events in a transaction
	l.mu.Lock()
	defer l.mu.Unlock()

	tx, err := l.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO audit_events (
			timestamp, profile, event_type, tool_name, arguments,
			duration_ms, status_code, success, error_msg, client_addr,
			request_size, response_size
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, event := range events {
		var argsJSON []byte
		if event.Arguments != nil {
			argsJSON, _ = json.Marshal(event.Arguments)
		}

		_, err := stmt.Exec(
			event.Timestamp,
			event.Profile,
			event.EventType,
			event.ToolName,
			string(argsJSON),
			event.DurationMs,
			event.StatusCode,
			event.Success,
			event.ErrorMsg,
			event.ClientAddr,
			event.RequestSize,
			event.ResponseSize,
		)
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}
	}

	return tx.Commit()
}

// backgroundFlush flushes the buffer periodically
func (l *Logger) backgroundFlush() {
	for range l.flushTicker.C {
		_ = l.Flush()
	}
}

// QueryOptions represents query parameters for retrieving audit events
type QueryOptions struct {
	Profile    string
	EventType  string
	ToolName   string
	StartTime  time.Time
	EndTime    time.Time
	Success    *bool
	Limit      int
	Offset     int
	OrderBy    string // "timestamp", "duration_ms"
	OrderDir   string // "ASC", "DESC"
}

// Query retrieves audit events based on filters
func (l *Logger) Query(opts QueryOptions) ([]Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	query := `
		SELECT id, timestamp, profile, event_type, tool_name, arguments,
		       duration_ms, status_code, success, error_msg, client_addr,
		       request_size, response_size
		FROM audit_events
		WHERE 1=1
	`
	args := make([]interface{}, 0)

	if opts.Profile != "" {
		query += " AND profile = ?"
		args = append(args, opts.Profile)
	}
	if opts.EventType != "" {
		query += " AND event_type = ?"
		args = append(args, opts.EventType)
	}
	if opts.ToolName != "" {
		query += " AND tool_name = ?"
		args = append(args, opts.ToolName)
	}
	if !opts.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, opts.StartTime)
	}
	if !opts.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, opts.EndTime)
	}
	if opts.Success != nil {
		query += " AND success = ?"
		args = append(args, *opts.Success)
	}

	// Order by
	orderBy := "timestamp"
	if opts.OrderBy != "" {
		orderBy = opts.OrderBy
	}
	orderDir := "DESC"
	if opts.OrderDir != "" {
		orderDir = opts.OrderDir
	}
	query += fmt.Sprintf(" ORDER BY %s %s", orderBy, orderDir)

	// Limit and offset
	limit := 100
	if opts.Limit > 0 {
		limit = opts.Limit
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var argsJSON sql.NullString

		err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.Profile,
			&event.EventType,
			&event.ToolName,
			&argsJSON,
			&event.DurationMs,
			&event.StatusCode,
			&event.Success,
			&event.ErrorMsg,
			&event.ClientAddr,
			&event.RequestSize,
			&event.ResponseSize,
		)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		if argsJSON.Valid && argsJSON.String != "" {
			_ = json.Unmarshal([]byte(argsJSON.String), &event.Arguments)
		}

		events = append(events, event)
	}

	return events, nil
}

// GetStats returns aggregated statistics
func (l *Logger) GetStats(profile string, since time.Time) (*Stats, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	query := `
		SELECT
			COUNT(*) as total_requests,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as successful_requests,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as failed_requests,
			AVG(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END) as avg_duration_ms,
			MAX(duration_ms) as max_duration_ms,
			MIN(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END) as min_duration_ms
		FROM audit_events
		WHERE event_type = 'execute'
	`
	args := make([]interface{}, 0)

	if profile != "" {
		query += " AND profile = ?"
		args = append(args, profile)
	}
	if !since.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, since)
	}

	var stats Stats
	var avgDuration, minDuration sql.NullFloat64

	err := l.db.QueryRow(query, args...).Scan(
		&stats.TotalRequests,
		&stats.SuccessfulRequests,
		&stats.FailedRequests,
		&avgDuration,
		&stats.MaxDurationMs,
		&minDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("query stats: %w", err)
	}

	if avgDuration.Valid {
		stats.AvgDurationMs = int64(avgDuration.Float64)
	}
	if minDuration.Valid {
		stats.MinDurationMs = int64(minDuration.Float64)
	}

	// Calculate error rate
	if stats.TotalRequests > 0 {
		stats.ErrorRate = float64(stats.FailedRequests) / float64(stats.TotalRequests) * 100
	}

	return &stats, nil
}

// Stats represents aggregated statistics
type Stats struct {
	TotalRequests       int64   `json:"total_requests"`
	SuccessfulRequests  int64   `json:"successful_requests"`
	FailedRequests      int64   `json:"failed_requests"`
	ErrorRate           float64 `json:"error_rate"`
	AvgDurationMs       int64   `json:"avg_duration_ms"`
	MaxDurationMs       int64   `json:"max_duration_ms"`
	MinDurationMs       int64   `json:"min_duration_ms"`
}

// Close closes the audit logger and flushes any remaining events
func (l *Logger) Close() error {
	if l.flushTicker != nil {
		l.flushTicker.Stop()
	}

	// Final flush
	if err := l.Flush(); err != nil {
		return err
	}

	return l.db.Close()
}
