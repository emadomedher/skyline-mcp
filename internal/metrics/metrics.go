package metrics

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Collector collects metrics for Prometheus export
type Collector struct {
	// Counters
	totalRequests     atomic.Int64
	successRequests   atomic.Int64
	failedRequests    atomic.Int64
	totalConnections  atomic.Int64
	activeConnections atomic.Int64

	// Per-profile counters
	profileRequests map[string]*atomic.Int64
	profileMu       sync.RWMutex

	// Per-tool counters
	toolRequests map[string]*atomic.Int64
	toolMu       sync.RWMutex

	// Duration histogram
	durationBuckets map[float64]*atomic.Int64 // milliseconds
	durationSum     atomic.Int64
	durationCount   atomic.Int64
	durationMu      sync.RWMutex

	// Start time
	startTime time.Time
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		profileRequests: make(map[string]*atomic.Int64),
		toolRequests:    make(map[string]*atomic.Int64),
		durationBuckets: initDurationBuckets(),
		startTime:       time.Now(),
	}
}

// initDurationBuckets initializes histogram buckets (in milliseconds)
func initDurationBuckets() map[float64]*atomic.Int64 {
	buckets := []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}
	m := make(map[float64]*atomic.Int64)
	for _, b := range buckets {
		counter := &atomic.Int64{}
		m[b] = counter
	}
	return m
}

// RecordRequest records a request metric
func (c *Collector) RecordRequest(profile, tool string, duration time.Duration, success bool) {
	c.totalRequests.Add(1)

	if success {
		c.successRequests.Add(1)
	} else {
		c.failedRequests.Add(1)
	}

	// Per-profile counter
	c.profileMu.Lock()
	if _, ok := c.profileRequests[profile]; !ok {
		c.profileRequests[profile] = &atomic.Int64{}
	}
	c.profileRequests[profile].Add(1)
	c.profileMu.Unlock()

	// Per-tool counter
	c.toolMu.Lock()
	if _, ok := c.toolRequests[tool]; !ok {
		c.toolRequests[tool] = &atomic.Int64{}
	}
	c.toolRequests[tool].Add(1)
	c.toolMu.Unlock()

	// Duration histogram
	durationMs := float64(duration.Milliseconds())
	c.durationSum.Add(duration.Milliseconds())
	c.durationCount.Add(1)

	c.durationMu.RLock()
	for bucket, counter := range c.durationBuckets {
		if durationMs <= bucket {
			counter.Add(1)
		}
	}
	c.durationMu.RUnlock()
}

// RecordConnection records a connection event
func (c *Collector) RecordConnection(connected bool) {
	c.totalConnections.Add(1)
	if connected {
		c.activeConnections.Add(1)
	} else {
		c.activeConnections.Add(-1)
	}
}

// PrometheusFormat exports metrics in Prometheus text format
func (c *Collector) PrometheusFormat() string {
	var output string

	// Total requests
	output += fmt.Sprintf("# HELP skyline_requests_total Total number of requests\n")
	output += fmt.Sprintf("# TYPE skyline_requests_total counter\n")
	output += fmt.Sprintf("skyline_requests_total %d\n\n", c.totalRequests.Load())

	// Successful requests
	output += fmt.Sprintf("# HELP skyline_requests_success_total Total number of successful requests\n")
	output += fmt.Sprintf("# TYPE skyline_requests_success_total counter\n")
	output += fmt.Sprintf("skyline_requests_success_total %d\n\n", c.successRequests.Load())

	// Failed requests
	output += fmt.Sprintf("# HELP skyline_requests_failed_total Total number of failed requests\n")
	output += fmt.Sprintf("# TYPE skyline_requests_failed_total counter\n")
	output += fmt.Sprintf("skyline_requests_failed_total %d\n\n", c.failedRequests.Load())

	// Per-profile requests
	output += fmt.Sprintf("# HELP skyline_requests_by_profile_total Total number of requests per profile\n")
	output += fmt.Sprintf("# TYPE skyline_requests_by_profile_total counter\n")
	c.profileMu.RLock()
	for profile, counter := range c.profileRequests {
		output += fmt.Sprintf("skyline_requests_by_profile_total{profile=\"%s\"} %d\n", profile, counter.Load())
	}
	c.profileMu.RUnlock()
	output += "\n"

	// Per-tool requests
	output += fmt.Sprintf("# HELP skyline_requests_by_tool_total Total number of requests per tool\n")
	output += fmt.Sprintf("# TYPE skyline_requests_by_tool_total counter\n")
	c.toolMu.RLock()
	for tool, counter := range c.toolRequests {
		output += fmt.Sprintf("skyline_requests_by_tool_total{tool=\"%s\"} %d\n", tool, counter.Load())
	}
	c.toolMu.RUnlock()
	output += "\n"

	// Active connections
	output += fmt.Sprintf("# HELP skyline_connections_active Number of active WebSocket connections\n")
	output += fmt.Sprintf("# TYPE skyline_connections_active gauge\n")
	output += fmt.Sprintf("skyline_connections_active %d\n\n", c.activeConnections.Load())

	// Total connections
	output += fmt.Sprintf("# HELP skyline_connections_total Total number of WebSocket connections\n")
	output += fmt.Sprintf("# TYPE skyline_connections_total counter\n")
	output += fmt.Sprintf("skyline_connections_total %d\n\n", c.totalConnections.Load())

	// Duration histogram
	output += fmt.Sprintf("# HELP skyline_request_duration_milliseconds Request duration in milliseconds\n")
	output += fmt.Sprintf("# TYPE skyline_request_duration_milliseconds histogram\n")
	c.durationMu.RLock()
	cumulativeCount := int64(0)
	for _, bucket := range []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000} {
		if counter, ok := c.durationBuckets[bucket]; ok {
			cumulativeCount += counter.Load()
			output += fmt.Sprintf("skyline_request_duration_milliseconds_bucket{le=\"%.0f\"} %d\n", bucket, cumulativeCount)
		}
	}
	c.durationMu.RUnlock()
	output += fmt.Sprintf("skyline_request_duration_milliseconds_bucket{le=\"+Inf\"} %d\n", c.durationCount.Load())
	output += fmt.Sprintf("skyline_request_duration_milliseconds_sum %d\n", c.durationSum.Load())
	output += fmt.Sprintf("skyline_request_duration_milliseconds_count %d\n\n", c.durationCount.Load())

	// Uptime
	uptime := time.Since(c.startTime).Seconds()
	output += fmt.Sprintf("# HELP skyline_uptime_seconds Uptime in seconds\n")
	output += fmt.Sprintf("# TYPE skyline_uptime_seconds counter\n")
	output += fmt.Sprintf("skyline_uptime_seconds %.0f\n\n", uptime)

	return output
}

// Snapshot returns a snapshot of current metrics
type Snapshot struct {
	TotalRequests     int64              `json:"total_requests"`
	SuccessRequests   int64              `json:"success_requests"`
	FailedRequests    int64              `json:"failed_requests"`
	ActiveConnections int64              `json:"active_connections"`
	TotalConnections  int64              `json:"total_connections"`
	AvgDurationMs     float64            `json:"avg_duration_ms"`
	ProfileRequests   map[string]int64   `json:"profile_requests"`
	ToolRequests      map[string]int64   `json:"tool_requests"`
	UptimeSeconds     float64            `json:"uptime_seconds"`
}

// Snapshot returns a snapshot of current metrics
func (c *Collector) Snapshot() *Snapshot {
	snap := &Snapshot{
		TotalRequests:     c.totalRequests.Load(),
		SuccessRequests:   c.successRequests.Load(),
		FailedRequests:    c.failedRequests.Load(),
		ActiveConnections: c.activeConnections.Load(),
		TotalConnections:  c.totalConnections.Load(),
		ProfileRequests:   make(map[string]int64),
		ToolRequests:      make(map[string]int64),
		UptimeSeconds:     time.Since(c.startTime).Seconds(),
	}

	// Calculate average duration
	if c.durationCount.Load() > 0 {
		snap.AvgDurationMs = float64(c.durationSum.Load()) / float64(c.durationCount.Load())
	}

	// Copy profile requests
	c.profileMu.RLock()
	for profile, counter := range c.profileRequests {
		snap.ProfileRequests[profile] = counter.Load()
	}
	c.profileMu.RUnlock()

	// Copy tool requests
	c.toolMu.RLock()
	for tool, counter := range c.toolRequests {
		snap.ToolRequests[tool] = counter.Load()
	}
	c.toolMu.RUnlock()

	return snap
}
