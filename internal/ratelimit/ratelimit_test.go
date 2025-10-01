package ratelimit

import "testing"

func TestConnectionLimiterUpdate(t *testing.T) {
	limiter := NewConnectionLimiter(5, 10, 2)
	if !limiter.Allow() {
		t.Fatalf("expected allow")
	}
	limiter.Update(1, 1, 1)
	current, max, tokens := limiter.Stats()
	if max != 1 {
		t.Fatalf("unexpected max %d", max)
	}
	if current > max {
		t.Fatalf("current %d exceeds max %d", current, max)
	}
	if tokens > 1 {
		t.Fatalf("tokens not clamped: %f", tokens)
	}
}
