package circuitbreaker

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartsInClosedState(t *testing.T) {
	b := New("test-api", 5, 30*time.Second)
	if s := b.State(); s != Closed {
		t.Fatalf("expected Closed, got %s", s)
	}
}

func TestClosedStateAllowsRequests(t *testing.T) {
	b := New("test-api", 5, 30*time.Second)
	for i := 0; i < 10; i++ {
		if err := b.Allow(); err != nil {
			t.Fatalf("request %d should be allowed: %v", i, err)
		}
		b.RecordSuccess()
	}
}

func TestTripsToOpenAfterThreshold(t *testing.T) {
	b := New("test-api", 3, 30*time.Second)
	for i := 0; i < 3; i++ {
		if err := b.Allow(); err != nil {
			t.Fatalf("request %d should be allowed: %v", i, err)
		}
		b.RecordFailure(fmt.Errorf("fail %d", i+1))
	}
	if s := b.State(); s != Open {
		t.Fatalf("expected Open, got %s", s)
	}
}

func TestOpenStateRejectsRequests(t *testing.T) {
	b := New("test-api", 2, 30*time.Second)
	for i := 0; i < 2; i++ {
		_ = b.Allow()
		b.RecordFailure(fmt.Errorf("server error"))
	}

	err := b.Allow()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var circuitErr *ErrCircuitOpen
	if !errors.As(err, &circuitErr) {
		t.Fatalf("expected *ErrCircuitOpen, got %T: %v", err, err)
	}
	if circuitErr.Name != "test-api" {
		t.Fatalf("expected name test-api, got %s", circuitErr.Name)
	}
}

func TestTransitionsToHalfOpenAfterCooldown(t *testing.T) {
	now := time.Now()
	b := New("test-api", 2, 10*time.Second)
	b.nowFunc = func() time.Time { return now }

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		_ = b.Allow()
		b.RecordFailure(fmt.Errorf("fail"))
	}
	if s := b.State(); s != Open {
		t.Fatalf("expected Open, got %s", s)
	}

	// Advance time past cooldown.
	now = now.Add(11 * time.Second)

	// Allow() should transition to HalfOpen and allow the probe.
	if err := b.Allow(); err != nil {
		t.Fatalf("expected probe to be allowed: %v", err)
	}
	if s := b.State(); s != HalfOpen {
		t.Fatalf("expected HalfOpen, got %s", s)
	}
}

func TestHalfOpenAllowsOneRequest(t *testing.T) {
	now := time.Now()
	b := New("test-api", 2, 10*time.Second)
	b.nowFunc = func() time.Time { return now }

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		_ = b.Allow()
		b.RecordFailure(fmt.Errorf("fail"))
	}

	// Advance past cooldown.
	now = now.Add(11 * time.Second)

	// First call: probe is allowed.
	if err := b.Allow(); err != nil {
		t.Fatalf("probe should be allowed: %v", err)
	}

	// Second call while probe is in-flight: should be rejected.
	err := b.Allow()
	if err == nil {
		t.Fatal("concurrent request during half-open should be rejected")
	}
	var circuitErr *ErrCircuitOpen
	if !errors.As(err, &circuitErr) {
		t.Fatalf("expected *ErrCircuitOpen, got %T: %v", err, err)
	}
}

func TestSuccessfulProbeClosesCircuit(t *testing.T) {
	now := time.Now()
	b := New("test-api", 2, 10*time.Second)
	b.nowFunc = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		_ = b.Allow()
		b.RecordFailure(fmt.Errorf("fail"))
	}

	now = now.Add(11 * time.Second)
	_ = b.Allow() // probe

	b.RecordSuccess()
	if s := b.State(); s != Closed {
		t.Fatalf("expected Closed after successful probe, got %s", s)
	}

	// Requests should flow again.
	if err := b.Allow(); err != nil {
		t.Fatalf("request should be allowed after recovery: %v", err)
	}
}

func TestFailedProbeReopensCircuit(t *testing.T) {
	now := time.Now()
	b := New("test-api", 2, 10*time.Second)
	b.nowFunc = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		_ = b.Allow()
		b.RecordFailure(fmt.Errorf("fail"))
	}

	now = now.Add(11 * time.Second)
	_ = b.Allow() // probe

	b.RecordFailure(fmt.Errorf("still failing"))

	// Should be Open again.
	if s := b.State(); s != Open {
		t.Fatalf("expected Open after failed probe, got %s", s)
	}

	// Should reject immediately.
	err := b.Allow()
	if err == nil {
		t.Fatal("expected rejection after failed probe")
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	b := New("test-api", 5, 30*time.Second)

	// 3 failures.
	for i := 0; i < 3; i++ {
		_ = b.Allow()
		b.RecordFailure(fmt.Errorf("fail"))
	}
	if s := b.State(); s != Closed {
		t.Fatalf("should still be Closed after 3 failures (threshold 5), got %s", s)
	}

	// 1 success resets the counter.
	_ = b.Allow()
	b.RecordSuccess()

	// 3 more failures should NOT trip (3 < 5).
	for i := 0; i < 3; i++ {
		_ = b.Allow()
		b.RecordFailure(fmt.Errorf("fail"))
	}
	if s := b.State(); s != Closed {
		t.Fatalf("should still be Closed (3+reset+3 < 5), got %s", s)
	}
}

func TestConcurrentAccessSafety(t *testing.T) {
	b := New("test-api", 100, 1*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = b.Allow()
			if n%3 == 0 {
				b.RecordFailure(fmt.Errorf("fail %d", n))
			} else {
				b.RecordSuccess()
			}
			_ = b.State()
			_ = b.Stats()
		}(i)
	}
	wg.Wait()

	// No panic or data race is the success criterion.
	// The state should be deterministic based on execution order, but
	// we can't predict that with goroutines — just verify no crash.
	stats := b.Stats()
	if stats.TotalFailures+stats.TotalSuccesses != 100 {
		t.Fatalf("expected 100 total operations, got %d", stats.TotalFailures+stats.TotalSuccesses)
	}
}

func TestDisabledBreakerNeverTrips(t *testing.T) {
	b := New("test-api", 0, 30*time.Second)

	for i := 0; i < 100; i++ {
		if err := b.Allow(); err != nil {
			t.Fatalf("disabled breaker should never reject: %v", err)
		}
		b.RecordFailure(fmt.Errorf("fail %d", i))
	}
	if s := b.State(); s != Closed {
		t.Fatalf("disabled breaker should stay Closed, got %s", s)
	}
}

func TestErrorMessageIncludesUsefulInfo(t *testing.T) {
	now := time.Now()
	b := New("my-api", 1, 30*time.Second)
	b.nowFunc = func() time.Time { return now }

	_ = b.Allow()
	b.RecordFailure(fmt.Errorf("connection refused"))

	now = now.Add(5 * time.Second)
	err := b.Allow()
	if err == nil {
		t.Fatal("expected error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "my-api") {
		t.Fatalf("error should contain API name: %s", msg)
	}
	if !strings.Contains(msg, "connection refused") {
		t.Fatalf("error should contain last error: %s", msg)
	}
	if !strings.Contains(msg, "ago") {
		t.Fatalf("error should contain time since failure: %s", msg)
	}
	if !strings.Contains(msg, "Retrying in") {
		t.Fatalf("error should contain retry info: %s", msg)
	}
	if !strings.Contains(msg, "try again later") {
		t.Fatalf("error should tell the agent to try again later: %s", msg)
	}
}

func TestStatsReflectsState(t *testing.T) {
	b := New("test-api", 2, 30*time.Second)

	// Initial stats.
	stats := b.Stats()
	if stats.State != "closed" {
		t.Fatalf("expected closed state, got %s", stats.State)
	}
	if stats.TotalFailures != 0 || stats.TotalSuccesses != 0 {
		t.Fatalf("expected zero counters")
	}

	_ = b.Allow()
	b.RecordSuccess()
	stats = b.Stats()
	if stats.TotalSuccesses != 1 {
		t.Fatalf("expected 1 success, got %d", stats.TotalSuccesses)
	}

	_ = b.Allow()
	b.RecordFailure(fmt.Errorf("oops"))
	_ = b.Allow()
	b.RecordFailure(fmt.Errorf("oops again"))

	stats = b.Stats()
	if stats.State != "open" {
		t.Fatalf("expected open state, got %s", stats.State)
	}
	if stats.TotalFailures != 2 {
		t.Fatalf("expected 2 failures, got %d", stats.TotalFailures)
	}
	if stats.ConsecutiveFails != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", stats.ConsecutiveFails)
	}
	if stats.LastFailureError != "oops again" {
		t.Fatalf("expected last error 'oops again', got %s", stats.LastFailureError)
	}
	if stats.LastFailureTime == "" {
		t.Fatal("expected non-empty last failure time")
	}
}

func TestStateStringRepresentation(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Closed, "closed"},
		{Open, "open"},
		{HalfOpen, "half-open"},
		{State(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestNilErrorInRecordFailure(t *testing.T) {
	b := New("test-api", 2, 30*time.Second)
	_ = b.Allow()
	b.RecordFailure(nil)

	stats := b.Stats()
	if stats.LastFailureError != "unknown error" {
		t.Fatalf("expected 'unknown error', got %q", stats.LastFailureError)
	}
}
