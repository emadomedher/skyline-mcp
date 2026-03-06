// Package polling provides a generic timer-based polling engine.
// It periodically calls registered data sources, diffs the response
// against the last snapshot, and notifies subscribers when data changes.
//
// The engine is protocol-agnostic — any data source implementing
// PollSource can be registered. Email inbox polling is the first
// consumer; API tool polling (e.g., "GET /tasks" on Jira) will follow.
package polling

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// PollSource is a data source that can be polled periodically.
type PollSource interface {
	// ID returns a unique identifier for this source (e.g., "email:inbox:user@example.com").
	ID() string

	// Fetch retrieves the current state of the data source.
	// The returned value will be JSON-marshaled for diffing.
	Fetch(ctx context.Context) (any, error)
}

// Notifier is called when a poll source detects a change.
type Notifier interface {
	// OnPollChanged is called with the source ID and the new data.
	OnPollChanged(sourceID string, data any)
}

// NotifierFunc adapts a plain function to the Notifier interface.
type NotifierFunc func(sourceID string, data any)

func (f NotifierFunc) OnPollChanged(sourceID string, data any) { f(sourceID, data) }

// Job represents a single poll job with its source, interval, and state.
type Job struct {
	Source   PollSource
	Interval time.Duration

	mu       sync.Mutex
	lastHash [32]byte
	lastData any
	running  bool
	cancel   context.CancelFunc
}

// Engine manages multiple poll jobs and dispatches change notifications.
type Engine struct {
	mu       sync.Mutex
	jobs     map[string]*Job
	notifier Notifier
	logger   *slog.Logger
	ctx      context.Context
	cancel   context.CancelFunc
}

// New creates a polling engine. The notifier is called whenever any
// source detects a change. Pass nil for a no-op notifier.
func New(logger *slog.Logger, notifier Notifier) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	if notifier == nil {
		notifier = NotifierFunc(func(string, any) {})
	}
	return &Engine{
		jobs:     make(map[string]*Job),
		notifier: notifier,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Register adds a poll source with the given interval.
// If a source with the same ID already exists, it is replaced.
func (e *Engine) Register(source PollSource, interval time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	id := source.ID()

	// Stop existing job if any
	if existing, ok := e.jobs[id]; ok {
		existing.stop()
	}

	job := &Job{
		Source:   source,
		Interval: interval,
	}
	e.jobs[id] = job

	e.logger.Info("poll job registered", "source", id, "interval", interval)

	// Start polling in background
	go e.runJob(job)
}

// Unregister removes and stops a poll source by ID.
func (e *Engine) Unregister(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if job, ok := e.jobs[id]; ok {
		job.stop()
		delete(e.jobs, id)
		e.logger.Info("poll job unregistered", "source", id)
	}
}

// Stop shuts down the engine and all running poll jobs.
func (e *Engine) Stop() {
	e.cancel()
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, job := range e.jobs {
		job.stop()
		delete(e.jobs, id)
	}
	e.logger.Info("polling engine stopped")
}

// Jobs returns the number of active poll jobs.
func (e *Engine) Jobs() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.jobs)
}

// runJob runs a single poll loop for a job until cancelled.
func (e *Engine) runJob(job *Job) {
	job.mu.Lock()
	if job.running {
		job.mu.Unlock()
		return
	}
	job.running = true
	jobCtx, cancel := context.WithCancel(e.ctx)
	job.cancel = cancel
	job.mu.Unlock()

	_ = job.Source.ID() // for debug logging if needed
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	// Initial poll immediately
	e.poll(jobCtx, job)

	for {
		select {
		case <-jobCtx.Done():
			return
		case <-ticker.C:
			e.poll(jobCtx, job)
		}
	}
}

// poll executes a single poll cycle: fetch, hash, diff, notify.
func (e *Engine) poll(ctx context.Context, job *Job) {
	sourceID := job.Source.ID()

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	data, err := job.Source.Fetch(fetchCtx)
	if err != nil {
		e.logger.Warn("poll fetch failed", "source", sourceID, "error", err)
		return
	}

	// Hash the result for efficient diffing
	raw, err := json.Marshal(data)
	if err != nil {
		e.logger.Warn("poll marshal failed", "source", sourceID, "error", err)
		return
	}
	hash := sha256.Sum256(raw)

	job.mu.Lock()
	changed := job.lastHash != hash
	isFirst := job.lastData == nil
	job.lastHash = hash
	job.lastData = data
	job.mu.Unlock()

	if changed && !isFirst {
		e.logger.Info("poll detected change", "source", sourceID)
		e.notifier.OnPollChanged(sourceID, data)
	} else if isFirst {
		e.logger.Debug("poll initial snapshot", "source", sourceID, "bytes", len(raw))
	}
}

func (j *Job) stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
	j.running = false
}

// Snapshot returns the last polled data for a source, or nil if not available.
func (e *Engine) Snapshot(sourceID string) any {
	e.mu.Lock()
	job, ok := e.jobs[sourceID]
	e.mu.Unlock()
	if !ok {
		return nil
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return job.lastData
}

// SourceID builds a standard source ID string.
func SourceID(protocol, resource, account string) string {
	return fmt.Sprintf("%s:%s:%s", protocol, resource, account)
}
