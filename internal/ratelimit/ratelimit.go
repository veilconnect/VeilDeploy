package ratelimit

import (
	"sync"
	"time"
)

// ConnectionLimiter manages connection rate limiting and max connections
type ConnectionLimiter struct {
	mu sync.Mutex

	// Max concurrent connections
	maxConnections int
	currentCount   int

	// Rate limiting with token bucket
	rate       int     // tokens per minute
	burst      int     // max tokens
	tokens     float64 // current tokens
	lastRefill time.Time
}

// NewConnectionLimiter creates a new connection limiter
func NewConnectionLimiter(maxConnections, ratePerMinute, burst int) *ConnectionLimiter {
	return &ConnectionLimiter{
		maxConnections: maxConnections,
		rate:           ratePerMinute,
		burst:          burst,
		tokens:         float64(burst),
		lastRefill:     time.Now(),
	}
}

// Allow checks if a new connection is allowed
func (cl *ConnectionLimiter) Allow() bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	// Check max connections
	if cl.currentCount >= cl.maxConnections {
		return false
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(cl.lastRefill)
	tokensToAdd := float64(cl.rate) * elapsed.Minutes()
	cl.tokens += tokensToAdd
	if cl.tokens > float64(cl.burst) {
		cl.tokens = float64(cl.burst)
	}
	cl.lastRefill = now

	// Check rate limit
	if cl.tokens < 1.0 {
		return false
	}

	// Consume a token
	cl.tokens -= 1.0
	cl.currentCount++
	return true
}

// Release decrements the connection count
func (cl *ConnectionLimiter) Release() {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.currentCount > 0 {
		cl.currentCount--
	}
}

// Stats returns current statistics
func (cl *ConnectionLimiter) Stats() (current, max int, tokens float64) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.currentCount, cl.maxConnections, cl.tokens
}
func (cl *ConnectionLimiter) Update(maxConnections, ratePerMinute, burst int) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if maxConnections > 0 {
		cl.maxConnections = maxConnections
	}
	if ratePerMinute > 0 {
		cl.rate = ratePerMinute
	}
	if burst > 0 {
		cl.burst = burst
	}
	if cl.tokens > float64(cl.burst) {
		cl.tokens = float64(cl.burst)
	}
	if cl.currentCount > cl.maxConnections {
		cl.currentCount = cl.maxConnections
	}
}
