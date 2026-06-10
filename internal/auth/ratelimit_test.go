package auth

import (
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterMaxFailures(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < maxLoginFailures; i++ {
		if l.tooMany("user:alice") {
			t.Fatalf("blocked after %d failures, want %d", i, maxLoginFailures)
		}
		l.recordFailure("user:alice")
	}
	if !l.tooMany("user:alice") {
		t.Fatal("expected limiter to block after max failures")
	}
	// Other keys are unaffected.
	if l.tooMany("user:bob") {
		t.Fatal("unrelated key should not be blocked")
	}
	// Failures age out after the window.
	now = now.Add(loginFailureWindow + time.Second)
	if l.tooMany("user:alice") {
		t.Fatal("expected failures to expire after the window")
	}
}

func TestLoginLimiterResetOnSuccess(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < maxLoginFailures; i++ {
		l.recordFailure("ip:1.2.3.4", "user:alice")
	}
	if !l.tooMany("ip:1.2.3.4") {
		t.Fatal("expected block before reset")
	}
	l.reset("ip:1.2.3.4", "user:alice")
	if l.tooMany("ip:1.2.3.4") || l.tooMany("user:alice") {
		t.Fatal("expected keys to be clear after reset")
	}
}

func TestLoginLimiterAnyKeyBlocks(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < maxLoginFailures; i++ {
		l.recordFailure("user:alice")
	}
	// A request carrying a fresh IP but the throttled username must
	// still be rejected (distributed brute force on one account).
	if !l.tooMany("ip:9.9.9.9", "user:alice") {
		t.Fatal("expected throttled username to block regardless of IP")
	}
}
