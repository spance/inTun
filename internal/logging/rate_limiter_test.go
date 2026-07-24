package logging

import (
	"testing"
	"time"
)

func TestRateLimiterReportsSuppressedEvents(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := NewRateLimiter(5 * time.Second)
	limiter.now = func() time.Time { return now }

	if allowed, suppressed := limiter.Allow("drop"); !allowed || suppressed != 0 {
		t.Fatalf("first event = %v/%d, want true/0", allowed, suppressed)
	}
	for range 3 {
		if allowed, _ := limiter.Allow("drop"); allowed {
			t.Fatal("event inside interval should be suppressed")
		}
	}
	now = now.Add(5 * time.Second)
	if allowed, suppressed := limiter.Allow("drop"); !allowed || suppressed != 3 {
		t.Fatalf("next event = %v/%d, want true/3", allowed, suppressed)
	}
}
