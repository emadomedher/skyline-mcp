// Package circuitbreaker provides a per-API circuit breaker that prevents
// cascading failures by short-circuiting requests to unhealthy upstream APIs.
//
// Three states:
//   - Closed  – requests pass through; consecutive failures are tracked.
//   - Open    – all requests fail immediately; no upstream calls are made.
//   - HalfOpen – one probe request is allowed through to test recovery.
package circuitbreaker

import (
	"fmt"
	"sync"
	"time"
)

// State represents the current circuit breaker state.
type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open and rejecting requests.
type ErrCircuitOpen struct {
	Name    string
	LastErr string
	Since   time.Duration // time since last failure
	RetryIn time.Duration // time until next probe attempt
}

func (e *ErrCircuitOpen) Error() string {
	return fmt.Sprintf(
		"circuit breaker open for API '%s' — last failure: %s (%s ago). Retrying in %ds. "+
			"The upstream API is currently unavailable; try again later.",
		e.Name,
		e.LastErr,
		e.Since.Truncate(time.Second),
		int(e.RetryIn.Seconds()),
	)
}

// Stats provides observability data about a circuit breaker's state.
type Stats struct {
	State            string `json:"state"`
	ConsecutiveFails int    `json:"consecutive_failures"`
	TotalFailures    int64  `json:"total_failures"`
	TotalSuccesses   int64  `json:"total_successes"`
	LastFailureTime  string `json:"last_failure_time,omitempty"`
	LastFailureError string `json:"last_failure_error,omitempty"`
}

// Breaker implements a thread-safe circuit breaker for a single API.
type Breaker struct {
	mu sync.Mutex

	name             string
	failureThreshold int
	cooldown         time.Duration

	state            State
	consecutiveFails int
	totalFailures    int64
	totalSuccesses   int64
	lastFailureTime  time.Time
	lastFailureErr   string
	openedAt         time.Time

	// nowFunc allows tests to inject a fake clock.
	nowFunc func() time.Time
}

// New creates a circuit breaker.
// failureThreshold: consecutive failures before tripping (0 = disabled).
// cooldown: time to wait in Open state before transitioning to HalfOpen.
func New(name string, failureThreshold int, cooldown time.Duration) *Breaker {
	return &Breaker{
		name:             name,
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		state:            Closed,
		nowFunc:          time.Now,
	}
}

// Allow checks if a request should be allowed through.
// Returns nil if the request is allowed, or *ErrCircuitOpen if the circuit is open.
func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Disabled breaker always allows requests.
	if b.failureThreshold <= 0 {
		return nil
	}

	now := b.nowFunc()

	switch b.state {
	case Closed:
		return nil

	case Open:
		elapsed := now.Sub(b.openedAt)
		if elapsed >= b.cooldown {
			// Transition to half-open: allow one probe.
			b.state = HalfOpen
			return nil
		}
		remaining := b.cooldown - elapsed
		since := now.Sub(b.lastFailureTime)
		return &ErrCircuitOpen{
			Name:    b.name,
			LastErr: b.lastFailureErr,
			Since:   since,
			RetryIn: remaining,
		}

	case HalfOpen:
		// In half-open state, only one probe request is allowed.
		// Subsequent concurrent requests while the probe is in-flight should be rejected.
		// We transition back to Open to block concurrent requests; the probe outcome
		// will set the final state via RecordSuccess or RecordFailure.
		b.state = Open
		b.openedAt = now // reset cooldown so concurrent requests see a fresh timer
		since := now.Sub(b.lastFailureTime)
		return &ErrCircuitOpen{
			Name:    b.name,
			LastErr: b.lastFailureErr,
			Since:   since,
			RetryIn: b.cooldown,
		}
	}

	return nil
}

// RecordSuccess records a successful request. Resets failure count and closes the circuit.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveFails = 0
	b.totalSuccesses++
	b.state = Closed
}

// RecordFailure records a failed request. May trip the circuit to Open.
func (b *Breaker) RecordFailure(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.nowFunc()
	b.consecutiveFails++
	b.totalFailures++
	b.lastFailureTime = now
	if err != nil {
		b.lastFailureErr = err.Error()
	} else {
		b.lastFailureErr = "unknown error"
	}

	// If disabled, never trip.
	if b.failureThreshold <= 0 {
		return
	}

	if b.consecutiveFails >= b.failureThreshold {
		b.state = Open
		b.openedAt = now
	}

	// If we were half-open and the probe failed, go back to Open.
	if b.state == HalfOpen {
		b.state = Open
		b.openedAt = now
	}
}

// State returns the current circuit breaker state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Stats returns observability data about the circuit breaker.
func (b *Breaker) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := Stats{
		State:            b.state.String(),
		ConsecutiveFails: b.consecutiveFails,
		TotalFailures:    b.totalFailures,
		TotalSuccesses:   b.totalSuccesses,
	}
	if !b.lastFailureTime.IsZero() {
		s.LastFailureTime = b.lastFailureTime.Format(time.RFC3339)
		s.LastFailureError = b.lastFailureErr
	}
	return s
}
