package logging

import (
	"sync"
	"time"
)

type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	events   map[string]rateLimitEvent
	now      func() time.Time
}

type rateLimitEvent struct {
	last       time.Time
	suppressed int
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	if interval <= 0 {
		interval = time.Second
	}
	return &RateLimiter{
		interval: interval,
		events:   make(map[string]rateLimitEvent),
		now:      time.Now,
	}
}

func (l *RateLimiter) Allow(key string) (allowed bool, suppressed int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	event := l.events[key]
	if event.last.IsZero() || now.Sub(event.last) >= l.interval {
		event.last = now
		suppressed = event.suppressed
		event.suppressed = 0
		l.events[key] = event
		return true, suppressed
	}
	event.suppressed++
	l.events[key] = event
	return false, 0
}
