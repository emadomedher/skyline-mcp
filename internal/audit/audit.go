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
	ID           int64                  `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	Profile      string                 `json:"profile"`
	EventType    string                 `json:"event_type"` // "execute", "connect", "disconnect", "error"
	APIName      string                 `json:"api_name,omitempty"`
	ToolName     string                 `json:"tool_name,omitempty"`
	Arguments    map[string]interface{} `json:"arguments,omitempty"`
	DurationMs   int64                  `json:"duration_ms,omitempty"`
	StatusCode   int                    `json:"status_code,omitempty"`
	Success      bool                   `json:"success"`
	ErrorMsg     string                 `json:"error_msg,omitempty"`
	ClientAddr   string                 `json:"client_addr,omitempty"`
	RequestSize  int64                  `json:"request_size,omitempty"`
	ResponseSize int64                  `json:"response_size,omitempty"`
}

// Logger handles audit logging to SQLite
type Logger struct {
	db          *sql.DB
	mu          sync.Mutex
	batchSize   int
	flushTicker *time.Ticker
	buffer      []Event
	bufferMu    sync.Mutex
	hub         *Hub
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
		api_name TEXT,
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

	// Migrate: add api_name column if it doesn't exist (for existing DBs)
	_, _ = db.Exec(`ALTER TABLE audit_events ADD COLUMN api_name TEXT`)
	// Index after migration so the column is guaranteed to exist
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_api_name ON audit_events(api_name)`)

	logger := &Logger{
		db:        db,
		batchSize: 100,
		buffer:    make([]Event, 0, 100),
		hub:       NewHub(),
	}

	// Start background flusher (every 5 seconds)
	logger.flushTicker = time.NewTicker(5 * time.Second)
	go logger.backgroundFlush()

	return logger, nil
}

// LogExecute logs a tool execution event
func (l *Logger) LogExecute(ctx context.Context, profile, apiName, toolName string, args map[string]interface{}, duration time.Duration, statusCode int, success bool, errMsg, clientAddr string, requestSize, responseSize int64) {
	event := Event{
		Timestamp:    time.Now(),
		Profile:      profile,
		EventType:    "execute",
		APIName:      apiName,
		ToolName:     toolName,
		Arguments:    args,
		DurationMs:   duration.Milliseconds(),
		StatusCode:   statusCode,
		Success:      success,
		ErrorMsg:     errMsg,
		ClientAddr:   clientAddr,
		RequestSize:  requestSize,
		ResponseSize: responseSize,
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

// EventHub returns the live event hub for real-time subscribers.
func (l *Logger) EventHub() *Hub {
	return l.hub
}

// bufferEvent adds an event to the buffer for batch insertion
// and broadcasts it to live subscribers.
func (l *Logger) bufferEvent(event Event) {
	// Broadcast to live subscribers first (non-blocking)
	l.hub.Publish(event)

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
			timestamp, profile, event_type, api_name, tool_name, arguments,
			duration_ms, status_code, success, error_msg, client_addr,
			request_size, response_size
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			event.APIName,
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
	APIName    string
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
		SELECT id, timestamp, profile, event_type, api_name, tool_name, arguments,
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
	if opts.APIName != "" {
		query += " AND api_name = ?"
		args = append(args, opts.APIName)
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
			&event.APIName,
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

	baseWhere := "WHERE event_type = 'execute'"
	args := make([]interface{}, 0)
	if profile != "" {
		baseWhere += " AND profile = ?"
		args = append(args, profile)
	}
	if !since.IsZero() {
		baseWhere += " AND timestamp >= ?"
		args = append(args, since)
	}

	// Overall totals
	totalsQuery := `
		SELECT
			COUNT(*) as total_requests,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as successful_requests,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as failed_requests,
			AVG(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END) as avg_duration_ms,
			MAX(duration_ms) as max_duration_ms,
			MIN(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END) as min_duration_ms,
			COALESCE(SUM(request_size), 0) as total_request_bytes,
			COALESCE(SUM(response_size), 0) as total_response_bytes
		FROM audit_events ` + baseWhere

	var stats Stats
	var avgDuration, minDuration sql.NullFloat64

	err := l.db.QueryRow(totalsQuery, args...).Scan(
		&stats.TotalRequests,
		&stats.SuccessfulRequests,
		&stats.FailedRequests,
		&avgDuration,
		&stats.MaxDurationMs,
		&minDuration,
		&stats.TotalRequestBytes,
		&stats.TotalResponseBytes,
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
	if stats.TotalRequests > 0 {
		stats.ErrorRate = float64(stats.FailedRequests) / float64(stats.TotalRequests) * 100
	}
	stats.EstRequestTokens = stats.TotalRequestBytes / 4
	stats.EstResponseTokens = stats.TotalResponseBytes / 4

	// Top APIs by call count
	topAPIsQuery := `
		SELECT
			COALESCE(api_name, '(unknown)') as name,
			COUNT(*) as calls,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as errors,
			AVG(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END) as avg_ms
		FROM audit_events ` + baseWhere + `
		GROUP BY api_name
		ORDER BY calls DESC
		LIMIT 10`

	rows, err := l.db.Query(topAPIsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query top apis: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a APIStats
		var avgMs sql.NullFloat64
		if err := rows.Scan(&a.Name, &a.Calls, &a.Errors, &avgMs); err != nil {
			return nil, fmt.Errorf("scan top api: %w", err)
		}
		if avgMs.Valid {
			a.AvgMs = int64(avgMs.Float64)
		}
		if a.Calls > 0 {
			a.ErrorRate = float64(a.Errors) / float64(a.Calls) * 100
		}
		stats.TopAPIs = append(stats.TopAPIs, a)
	}

	// Top tools by call count
	topToolsQuery := `
		SELECT
			COALESCE(tool_name, '(unknown)') as name,
			COUNT(*) as calls,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as errors,
			AVG(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END) as avg_ms
		FROM audit_events ` + baseWhere + `
		GROUP BY tool_name
		ORDER BY calls DESC
		LIMIT 10`

	rows2, err := l.db.Query(topToolsQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query top tools: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var a APIStats
		var avgMs sql.NullFloat64
		if err := rows2.Scan(&a.Name, &a.Calls, &a.Errors, &avgMs); err != nil {
			return nil, fmt.Errorf("scan top tool: %w", err)
		}
		if avgMs.Valid {
			a.AvgMs = int64(avgMs.Float64)
		}
		if a.Calls > 0 {
			a.ErrorRate = float64(a.Errors) / float64(a.Calls) * 100
		}
		stats.TopTools = append(stats.TopTools, a)
	}

	// Recent events (last 20)
	recentQuery := `
		SELECT id, timestamp, profile, event_type, api_name, tool_name, arguments,
		       duration_ms, status_code, success, error_msg, client_addr,
		       request_size, response_size
		FROM audit_events ` + baseWhere + `
		ORDER BY timestamp DESC
		LIMIT 20`

	rows3, err := l.db.Query(recentQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query recent events: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		var event Event
		var argsJSON sql.NullString
		if err := rows3.Scan(
			&event.ID, &event.Timestamp, &event.Profile, &event.EventType,
			&event.APIName, &event.ToolName, &argsJSON,
			&event.DurationMs, &event.StatusCode, &event.Success,
			&event.ErrorMsg, &event.ClientAddr, &event.RequestSize, &event.ResponseSize,
		); err != nil {
			return nil, fmt.Errorf("scan recent event: %w", err)
		}
		if argsJSON.Valid && argsJSON.String != "" {
			_ = json.Unmarshal([]byte(argsJSON.String), &event.Arguments)
		}
		stats.RecentEvents = append(stats.RecentEvents, event)
	}

	return &stats, nil
}

// Stats represents aggregated statistics
type Stats struct {
	TotalRequests      int64      `json:"total_requests"`
	SuccessfulRequests int64      `json:"successful_requests"`
	FailedRequests     int64      `json:"failed_requests"`
	ErrorRate          float64    `json:"error_rate"`
	AvgDurationMs      int64      `json:"avg_duration_ms"`
	MaxDurationMs      int64      `json:"max_duration_ms"`
	MinDurationMs      int64      `json:"min_duration_ms"`
	TotalRequestBytes  int64      `json:"total_request_bytes"`
	TotalResponseBytes int64      `json:"total_response_bytes"`
	EstRequestTokens   int64      `json:"est_request_tokens"`
	EstResponseTokens  int64      `json:"est_response_tokens"`
	TopAPIs            []APIStats `json:"top_apis"`
	TopTools           []APIStats `json:"top_tools"`
	RecentEvents       []Event    `json:"recent_events"`
}

// APIStats represents aggregated statistics for a single API or tool
type APIStats struct {
	Name       string  `json:"name"`
	Calls      int64   `json:"calls"`
	Errors     int64   `json:"errors"`
	ErrorRate  float64 `json:"error_rate"`
	AvgMs      int64   `json:"avg_ms"`
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
