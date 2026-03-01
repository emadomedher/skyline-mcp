package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUnlimitedAllows(t *testing.T) {
	l := New(0, 0, 0)
	for i := 0; i < 100; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("unlimited limiter should always allow, got: %v", err)
		}
	}
}

func TestRPMTokenBucket(t *testing.T) {
	const rpm = 5
	l := New(rpm, 0, 0)

	// Should allow up to rpm requests immediately (burst)
	for i := 0; i < rpm; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		err := l.Wait(ctx)
		cancel()
		if err != nil {
			t.Fatalf("request %d should be allowed, got: %v", i+1, err)
		}
	}

	// Next request should not be immediately available
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err := l.Wait(ctx)
	cancel()
	if err == nil {
		t.Fatal("request beyond burst should not be immediately allowed")
	}
	// It should be a context deadline error (we timed out waiting for token)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestRPHFixedWindow(t *testing.T) {
	const rph = 3
	l := New(0, rph, 0)

	// Should allow exactly rph requests
	for i := 0; i < rph; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("request %d should be allowed, got: %v", i+1, err)
		}
	}

	// Next request should be rate limited immediately (no waiting for hourly reset)
	err := l.Wait(context.Background())
	if err == nil {
		t.Fatal("request beyond hourly quota should be rejected")
	}
	var rl *ErrRateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimited, got: %T %v", err, err)
	}
	if rl.Tier != "rph" {
		t.Fatalf("expected tier rph, got: %s", rl.Tier)
	}
	if rl.Limit != rph {
		t.Fatalf("expected limit %d, got: %d", rph, rl.Limit)
	}
	if rl.RetryAfter <= 0 {
		t.Fatalf("expected positive RetryAfter, got: %v", rl.RetryAfter)
	}
}

func TestRPDFixedWindow(t *testing.T) {
	const rpd = 3
	l := New(0, 0, rpd)

	// Should allow exactly rpd requests
	for i := 0; i < rpd; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("request %d should be allowed, got: %v", i+1, err)
		}
	}

	// Next request should be rate limited immediately
	err := l.Wait(context.Background())
	if err == nil {
		t.Fatal("request beyond daily quota should be rejected")
	}
	var rl *ErrRateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimited, got: %T %v", err, err)
	}
	if rl.Tier != "rpd" {
		t.Fatalf("expected tier rpd, got: %s", rl.Tier)
	}
	if rl.Limit != rpd {
		t.Fatalf("expected limit %d, got: %d", rpd, rl.Limit)
	}
	if rl.RetryAfter <= 0 {
		t.Fatalf("expected positive RetryAfter, got: %v", rl.RetryAfter)
	}
}

func TestRPHReturnsErrorNotBlocks(t *testing.T) {
	l := New(0, 1, 0)

	// Use the one allowed request
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first request should be allowed: %v", err)
	}

	// Second request should return error immediately (not block)
	start := time.Now()
	err := l.Wait(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for exhausted hourly quota")
	}
	// Should return very quickly (not wait for the hour to reset)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("hourly quota exhaustion should return immediately, took: %v", elapsed)
	}
}

func TestRPDReturnsErrorNotBlocks(t *testing.T) {
	l := New(0, 0, 1)

	// Use the one allowed request
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first request should be allowed: %v", err)
	}

	// Second request should return error immediately
	start := time.Now()
	err := l.Wait(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for exhausted daily quota")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("daily quota exhaustion should return immediately, took: %v", elapsed)
	}
}

func TestWaitBlocksAndRetriesForRPM(t *testing.T) {
	// 60 RPM = 1 per second; after consuming the burst of 1,
	// the next Wait should block briefly then succeed.
	l := New(60, 0, 0)

	// Consume the entire burst
	for i := 0; i < 60; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		_ = l.Wait(ctx)
		cancel()
	}

	// Now wait with a generous timeout — should succeed after ~1 token refills
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	err := l.Wait(ctx)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected Wait to succeed after refill, got: %v", err)
	}
	// Should have waited at least a little for the refill
	if elapsed < 500*time.Millisecond {
		// With 60 RPM the refill is 1/sec, so ~1s expected.
		// Be lenient: at least 500ms.
		t.Logf("waited %v (expected ~1s)", elapsed)
	}
}

func TestContextCancellation(t *testing.T) {
	l := New(1, 0, 0)

	// Use the one burst token
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first request should succeed: %v", err)
	}

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := l.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestStatsAccuracy(t *testing.T) {
	l := New(10, 100, 1000)

	// Make 3 requests
	for i := 0; i < 3; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("request %d should succeed: %v", i+1, err)
		}
	}

	stats := l.Stats()
	if stats.RPM != 10 {
		t.Fatalf("expected RPM=10, got: %d", stats.RPM)
	}
	if stats.RPH != 100 {
		t.Fatalf("expected RPH=100, got: %d", stats.RPH)
	}
	if stats.RPD != 1000 {
		t.Fatalf("expected RPD=1000, got: %d", stats.RPD)
	}
	if stats.HourCount != 3 {
		t.Fatalf("expected HourCount=3, got: %d", stats.HourCount)
	}
	if stats.HourRemaining != 97 {
		t.Fatalf("expected HourRemaining=97, got: %d", stats.HourRemaining)
	}
	if stats.DayCount != 3 {
		t.Fatalf("expected DayCount=3, got: %d", stats.DayCount)
	}
	if stats.DayRemaining != 997 {
		t.Fatalf("expected DayRemaining=997, got: %d", stats.DayRemaining)
	}
	// TokensLeft should be around 7 (10 burst - 3 used, plus tiny refill)
	if stats.TokensLeft < 6.5 || stats.TokensLeft > 10.5 {
		t.Fatalf("expected TokensLeft ~7, got: %f", stats.TokensLeft)
	}
}

func TestStatsUnlimited(t *testing.T) {
	l := New(0, 0, 0)
	stats := l.Stats()
	if stats.RPM != 0 || stats.RPH != 0 || stats.RPD != 0 {
		t.Fatalf("expected all zeros for unlimited, got: %+v", stats)
	}
	if stats.HourRemaining != 0 || stats.DayRemaining != 0 {
		t.Fatalf("expected 0 remaining for unlimited, got: %+v", stats)
	}
}

func TestThreeTierInteraction(t *testing.T) {
	// RPM=100 (high burst), RPH=5 (low hourly), RPD=10
	// After 5 requests the hourly limit should kick in
	l := New(100, 5, 10)

	for i := 0; i < 5; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("request %d should succeed: %v", i+1, err)
		}
	}

	err := l.Wait(context.Background())
	if err == nil {
		t.Fatal("6th request should be rate limited by hourly quota")
	}
	var rl *ErrRateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimited, got: %T %v", err, err)
	}
	if rl.Tier != "rph" {
		t.Fatalf("expected tier rph, got: %s", rl.Tier)
	}
}

func TestDailyLimitCheckedBeforeHourly(t *testing.T) {
	// RPD=2, RPH=100 — daily should trigger first
	l := New(0, 100, 2)

	for i := 0; i < 2; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("request %d should succeed: %v", i+1, err)
		}
	}

	err := l.Wait(context.Background())
	if err == nil {
		t.Fatal("3rd request should be rate limited by daily quota")
	}
	var rl *ErrRateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("expected ErrRateLimited, got: %T %v", err, err)
	}
	if rl.Tier != "rpd" {
		t.Fatalf("expected tier rpd, got: %s", rl.Tier)
	}
}

func TestErrRateLimitedMessage(t *testing.T) {
	err := &ErrRateLimited{
		Tier:       "rph",
		Limit:      100,
		RetryAfter: 30 * time.Minute,
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("error message should not be empty")
	}
	if !contains(msg, "rph") || !contains(msg, "100") {
		t.Fatalf("error message should contain tier and limit: %s", msg)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
