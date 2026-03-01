// Package ratelimit provides per-API rate limiting using a token bucket
// for per-minute burst control and fixed window counters for per-hour
// and per-day quotas.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Limiter enforces per-minute (token bucket), per-hour (fixed window),
// and per-day (fixed window) rate limits.
type Limiter struct {
	rpm int // max requests per minute (0 = unlimited)
	rph int // max requests per hour (0 = unlimited)
	rpd int // max requests per day (0 = unlimited)

	mu sync.Mutex

	// Token bucket state (per-minute)
	tokens    float64
	maxTokens float64
	refillPer time.Duration // time between token refills
	lastRefil time.Time

	// Fixed window state (per-hour)
	hourCount int
	hourStart time.Time

	// Fixed window state (per-day)
	dayCount int
	dayStart time.Time
}

// New creates a rate limiter with the given per-minute, per-hour, and per-day limits.
// A value of 0 means unlimited for that tier.
func New(rpm, rph, rpd int) *Limiter {
	now := time.Now()
	l := &Limiter{
		rpm:       rpm,
		rph:       rph,
		rpd:       rpd,
		hourStart: now.Truncate(time.Hour),
		dayStart:  truncateToDay(now),
	}
	if rpm > 0 {
		l.tokens = float64(rpm)
		l.maxTokens = float64(rpm)
		l.refillPer = time.Minute / time.Duration(rpm)
		l.lastRefil = now
	}
	return l
}

// truncateToDay returns the start of the current day (midnight UTC).
func truncateToDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// ErrRateLimited is returned when a request is rejected by the rate limiter.
type ErrRateLimited struct {
	Tier       string        // "rpm", "rph", or "rpd"
	Limit      int           // the configured limit
	RetryAfter time.Duration // suggested wait time
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("rate limited (%s: %d max) — retry after %s", e.Tier, e.Limit, e.RetryAfter.Truncate(time.Second))
}

// Wait blocks until a token is available or the context is cancelled.
// Returns nil if the request is allowed, ErrRateLimited if the hourly or daily
// quota is exhausted (cannot wait for window reset), or context error if cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	// Fast path: no limits configured
	if l.rpm == 0 && l.rph == 0 && l.rpd == 0 {
		return nil
	}

	for {
		retryAfter, err := l.tryAcquire()
		if err != nil {
			return err // hourly/daily quota exhausted — don't wait
		}
		if retryAfter == 0 {
			return nil // token acquired
		}
		// Wait for token bucket refill
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryAfter):
			// retry acquire
		}
	}
}

// tryAcquire attempts to take a token. Returns (0, nil) on success,
// (retryAfter, nil) if the per-minute bucket is empty but can be waited on,
// or (0, ErrRateLimited) if the per-hour or per-day quota is exhausted.
func (l *Limiter) tryAcquire() (time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Check per-day fixed window first (longest window = least recoverable)
	if l.rpd > 0 {
		dayStart := truncateToDay(now)
		if dayStart != l.dayStart {
			l.dayCount = 0
			l.dayStart = dayStart
		}
		if l.dayCount >= l.rpd {
			retryAfter := l.dayStart.Add(24 * time.Hour).Sub(now)
			return 0, &ErrRateLimited{
				Tier:       "rpd",
				Limit:      l.rpd,
				RetryAfter: retryAfter,
			}
		}
	}

	// Check per-hour fixed window
	if l.rph > 0 {
		hourStart := now.Truncate(time.Hour)
		if hourStart != l.hourStart {
			l.hourCount = 0
			l.hourStart = hourStart
		}
		if l.hourCount >= l.rph {
			retryAfter := l.hourStart.Add(time.Hour).Sub(now)
			return 0, &ErrRateLimited{
				Tier:       "rph",
				Limit:      l.rph,
				RetryAfter: retryAfter,
			}
		}
	}

	// Check per-minute token bucket
	if l.rpm > 0 {
		// Refill tokens based on elapsed time
		elapsed := now.Sub(l.lastRefil)
		l.tokens += elapsed.Seconds() * (float64(l.rpm) / 60.0)
		if l.tokens > l.maxTokens {
			l.tokens = l.maxTokens
		}
		l.lastRefil = now

		if l.tokens < 1.0 {
			// How long until one token is available
			deficit := 1.0 - l.tokens
			refillTime := time.Duration(deficit / (float64(l.rpm) / 60.0) * float64(time.Second))
			if refillTime < time.Millisecond {
				refillTime = time.Millisecond
			}
			return refillTime, nil
		}

		l.tokens -= 1.0
	}

	// Increment hourly counter
	if l.rph > 0 {
		l.hourCount++
	}

	// Increment daily counter
	if l.rpd > 0 {
		l.dayCount++
	}

	return 0, nil
}

// Stats returns the current rate limiter state for observability.
type Stats struct {
	RPM           int     `json:"rpm"`
	RPH           int     `json:"rph"`
	RPD           int     `json:"rpd"`
	TokensLeft    float64 `json:"tokens_left"`
	HourCount     int     `json:"hour_count"`
	HourRemaining int     `json:"hour_remaining"`
	DayCount      int     `json:"day_count"`
	DayRemaining  int     `json:"day_remaining"`
}

func (l *Limiter) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Refresh token count for accurate display
	tokens := l.tokens
	if l.rpm > 0 {
		elapsed := now.Sub(l.lastRefil)
		tokens += elapsed.Seconds() * (float64(l.rpm) / 60.0)
		if tokens > l.maxTokens {
			tokens = l.maxTokens
		}
	}

	hourCount := l.hourCount
	if l.rph > 0 {
		hourStart := now.Truncate(time.Hour)
		if hourStart != l.hourStart {
			hourCount = 0
		}
	}

	dayCount := l.dayCount
	if l.rpd > 0 {
		dayStart := truncateToDay(now)
		if dayStart != l.dayStart {
			dayCount = 0
		}
	}

	hourRemaining := 0
	if l.rph > 0 {
		hourRemaining = l.rph - hourCount
		if hourRemaining < 0 {
			hourRemaining = 0
		}
	}

	dayRemaining := 0
	if l.rpd > 0 {
		dayRemaining = l.rpd - dayCount
		if dayRemaining < 0 {
			dayRemaining = 0
		}
	}

	return Stats{
		RPM:           l.rpm,
		RPH:           l.rph,
		RPD:           l.rpd,
		TokensLeft:    tokens,
		HourCount:     hourCount,
		HourRemaining: hourRemaining,
		DayCount:      dayCount,
		DayRemaining:  dayRemaining,
	}
}
