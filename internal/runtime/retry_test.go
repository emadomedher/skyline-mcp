package runtime

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestRetryDelayExponentialBackoff(t *testing.T) {
	// Without Retry-After, delays should increase exponentially.
	// attempt 0: 500ms * 2^0 + jitter = ~500-750ms
	// attempt 1: 500ms * 2^1 + jitter = ~1000-1250ms
	// attempt 2: 500ms * 2^2 + jitter = ~2000-2250ms
	// attempt 3: 500ms * 2^3 + jitter = ~4000-4250ms
	prev := time.Duration(0)
	for attempt := 0; attempt < 4; attempt++ {
		d := retryDelay(attempt, 0)
		if d <= prev && attempt > 0 {
			// Due to jitter the strict ordering could rarely fail,
			// but the base doubles each time so in practice it won't.
			t.Errorf("attempt %d: delay %v should be greater than previous %v", attempt, d, prev)
		}
		prev = d
	}
}

func TestRetryDelayIncludesJitter(t *testing.T) {
	// Run many iterations and verify the delay is not always the same (jitter).
	seen := map[time.Duration]bool{}
	for i := 0; i < 100; i++ {
		d := retryDelay(0, 0)
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected jitter to produce varying delays, got %d unique values", len(seen))
	}
}

func TestRetryDelayRespectsMaxCap(t *testing.T) {
	// Very high attempt number should be capped at 10s.
	d := retryDelay(20, 0)
	if d > retryMaxDelay {
		t.Errorf("delay %v exceeds max cap %v", d, retryMaxDelay)
	}
}

func TestRetryDelayUsesRetryAfter(t *testing.T) {
	// When Retry-After is provided, it should be used instead of backoff.
	d := retryDelay(0, 5*time.Second)
	if d != 5*time.Second {
		t.Errorf("expected 5s, got %v", d)
	}
}

func TestRetryDelayRetryAfterCapped(t *testing.T) {
	// Retry-After values exceeding 30s should be capped.
	d := retryDelay(0, 60*time.Second)
	if d != retryAfterCap {
		t.Errorf("expected %v (cap), got %v", retryAfterCap, d)
	}
}

func TestRetryDelayBoundsPerAttempt(t *testing.T) {
	tests := []struct {
		attempt int
		minMS   int // minimum expected delay in ms (base * 2^attempt, no jitter)
		maxMS   int // maximum expected delay in ms (base * 2^attempt + max jitter, or cap)
	}{
		{0, 500, 750},
		{1, 1000, 1250},
		{2, 2000, 2250},
		{3, 4000, 4250},
		{4, 8000, 8250},
		{5, 10000, 10000}, // capped at 10s
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			for i := 0; i < 50; i++ {
				d := retryDelay(tt.attempt, 0)
				ms := int(d.Milliseconds())
				if ms < tt.minMS || ms > tt.maxMS {
					t.Errorf("attempt %d: delay %dms not in [%d, %d]", tt.attempt, ms, tt.minMS, tt.maxMS)
				}
			}
		})
	}
}

func TestIsRetryableIdempotentOn5xx(t *testing.T) {
	idempotent := []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS"}
	retryableCodes := []int{500, 502, 503, 504}

	for _, method := range idempotent {
		for _, code := range retryableCodes {
			if !isRetryable(method, code, nil) {
				t.Errorf("expected %s+%d to be retryable", method, code)
			}
		}
	}
}

func TestIsRetryablePOSTOn503And429(t *testing.T) {
	if !isRetryable("POST", 503, nil) {
		t.Error("expected POST+503 to be retryable")
	}
	if !isRetryable("POST", 429, nil) {
		t.Error("expected POST+429 to be retryable")
	}
}

func TestIsRetryablePOSTNotRetryableOn500And502(t *testing.T) {
	if isRetryable("POST", 500, nil) {
		t.Error("expected POST+500 to NOT be retryable")
	}
	if isRetryable("POST", 502, nil) {
		t.Error("expected POST+502 to NOT be retryable")
	}
}

func TestIsRetryable4xxNotRetryable(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	codes := []int{400, 401, 403, 404, 405, 409, 422}

	for _, method := range methods {
		for _, code := range codes {
			if isRetryable(method, code, nil) {
				t.Errorf("expected %s+%d to NOT be retryable", method, code)
			}
		}
	}
}

func TestIsRetryable429RetryableForAllMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, method := range methods {
		if !isRetryable(method, 429, nil) {
			t.Errorf("expected %s+429 to be retryable", method)
		}
	}
}

func TestIsRetryableConnectionError(t *testing.T) {
	connErr := fmt.Errorf("dial tcp: connection refused")

	// Idempotent methods should retry on connection error.
	if !isRetryable("GET", 0, connErr) {
		t.Error("expected GET + connection error to be retryable")
	}
	if !isRetryable("PUT", 0, connErr) {
		t.Error("expected PUT + connection error to be retryable")
	}

	// Non-idempotent methods should NOT retry on connection error.
	if isRetryable("POST", 0, connErr) {
		t.Error("expected POST + connection error to NOT be retryable")
	}
	if isRetryable("PATCH", 0, connErr) {
		t.Error("expected PATCH + connection error to NOT be retryable")
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	d := parseRetryAfter("5")
	if d != 5*time.Second {
		t.Errorf("expected 5s, got %v", d)
	}
}

func TestParseRetryAfterZeroSeconds(t *testing.T) {
	d := parseRetryAfter("0")
	if d != 0 {
		t.Errorf("expected 0, got %v", d)
	}
}

func TestParseRetryAfterLargeValue(t *testing.T) {
	// 120 seconds exceeds 30s cap.
	d := parseRetryAfter("120")
	if d != retryAfterCap {
		t.Errorf("expected %v (cap), got %v", retryAfterCap, d)
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	// Use a date far enough in the future that time.Until is positive.
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	d := parseRetryAfter(future)
	// Should be roughly 10s (allow some slack for test execution time).
	if d < 8*time.Second || d > 12*time.Second {
		t.Errorf("expected ~10s, got %v", d)
	}
}

func TestParseRetryAfterHTTPDatePast(t *testing.T) {
	// A date in the past should return 0.
	past := time.Now().Add(-10 * time.Second).UTC().Format(http.TimeFormat)
	d := parseRetryAfter(past)
	if d != 0 {
		t.Errorf("expected 0 for past date, got %v", d)
	}
}

func TestParseRetryAfterHTTPDateCapped(t *testing.T) {
	// A date 60s in the future should be capped at 30s.
	future := time.Now().Add(60 * time.Second).UTC().Format(http.TimeFormat)
	d := parseRetryAfter(future)
	if d != retryAfterCap {
		t.Errorf("expected %v (cap), got %v", retryAfterCap, d)
	}
}

func TestParseRetryAfterUnparseable(t *testing.T) {
	cases := []string{"", "abc", "not-a-date", "-1"}
	for _, val := range cases {
		d := parseRetryAfter(val)
		if d != 0 {
			t.Errorf("parseRetryAfter(%q) = %v, want 0", val, d)
		}
	}
}

func TestParseRetryAfterWhitespace(t *testing.T) {
	d := parseRetryAfter("  10  ")
	if d != 10*time.Second {
		t.Errorf("expected 10s, got %v", d)
	}
}

func TestIsIdempotent(t *testing.T) {
	yes := []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS"}
	no := []string{"POST", "PATCH"}

	for _, m := range yes {
		if !isIdempotent(m) {
			t.Errorf("expected %s to be idempotent", m)
		}
	}
	for _, m := range no {
		if isIdempotent(m) {
			t.Errorf("expected %s to NOT be idempotent", m)
		}
	}
}
