package auth

import (
	"sync"
	"time"
)

// Login throttling parameters: after maxLoginFailures failed attempts
// within loginFailureWindow for the same key (client IP or username),
// further attempts are rejected until old failures age out.
const (
	maxLoginFailures   = 10
	loginFailureWindow = 15 * time.Minute
	// limiterMaxKeys bounds the tracking map so an attacker rotating
	// spoofed source addresses can't grow it without limit.
	limiterMaxKeys = 10000
)

// loginLimiter is a small in-memory sliding-window failure counter used
// to slow down credential brute-forcing. Keys are opaque strings; the
// handler tracks both the client IP and the targeted username so
// neither a single-IP dictionary attack nor a distributed attack on one
// account can hammer bcrypt unchecked.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	window   time.Duration
	max      int
	now      func() time.Time // injectable for tests
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		failures: make(map[string][]time.Time),
		window:   loginFailureWindow,
		max:      maxLoginFailures,
		now:      time.Now,
	}
}

// prune drops timestamps older than the window for key. Caller must
// hold l.mu. Returns the surviving entries.
func (l *loginLimiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	ts := l.failures[key]
	kept := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
	return kept
}

// tooMany reports whether any of the keys has reached the failure cap.
func (l *loginLimiter) tooMany(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, k := range keys {
		if k == "" {
			continue
		}
		if len(l.prune(k, now)) >= l.max {
			return true
		}
	}
	return false
}

// recordFailure adds one failed attempt to every key.
func (l *loginLimiter) recordFailure(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// Opportunistic global cleanup when the map gets large.
	if len(l.failures) >= limiterMaxKeys {
		for k := range l.failures {
			l.prune(k, now)
		}
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		// Even if still at capacity after cleanup, prefer recording over
		// unbounded growth: skip brand-new keys only when full.
		if _, exists := l.failures[k]; !exists && len(l.failures) >= limiterMaxKeys {
			continue
		}
		l.failures[k] = append(l.prune(k, now), now)
	}
}

// reset clears the counters for the keys (successful login).
func (l *loginLimiter) reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.failures, k)
	}
}
