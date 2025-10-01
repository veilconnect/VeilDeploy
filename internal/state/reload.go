package state

import (
	"sync"
	"time"
)

// ReloadEvent represents a configuration reload event
type ReloadEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	Changes   []string  `json:"changes,omitempty"`
}

// ReloadTracker tracks configuration reload history
type ReloadTracker struct {
	mu      sync.RWMutex
	history []ReloadEvent
	maxSize int
}

// NewReloadTracker creates a new reload tracker
func NewReloadTracker(maxSize int) *ReloadTracker {
	if maxSize <= 0 {
		maxSize = 10
	}
	return &ReloadTracker{
		history: make([]ReloadEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

// RecordSuccess records a successful reload
func (rt *ReloadTracker) RecordSuccess(changes []string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	event := ReloadEvent{
		Timestamp: time.Now(),
		Success:   true,
		Changes:   changes,
	}

	rt.addEvent(event)
}

// RecordFailure records a failed reload
func (rt *ReloadTracker) RecordFailure(err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	event := ReloadEvent{
		Timestamp: time.Now(),
		Success:   false,
		Error:     err.Error(),
	}

	rt.addEvent(event)
}

func (rt *ReloadTracker) addEvent(event ReloadEvent) {
	rt.history = append(rt.history, event)

	// Keep only the last maxSize events
	if len(rt.history) > rt.maxSize {
		rt.history = rt.history[1:]
	}
}

// GetHistory returns the reload history
func (rt *ReloadTracker) GetHistory() []ReloadEvent {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	// Return a copy
	result := make([]ReloadEvent, len(rt.history))
	copy(result, rt.history)
	return result
}

// GetLastReload returns the most recent reload event
func (rt *ReloadTracker) GetLastReload() *ReloadEvent {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	if len(rt.history) == 0 {
		return nil
	}

	// Return a copy
	event := rt.history[len(rt.history)-1]
	return &event
}

// Stats returns reload statistics
func (rt *ReloadTracker) Stats() (total, successful, failed int) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	total = len(rt.history)
	for _, event := range rt.history {
		if event.Success {
			successful++
		} else {
			failed++
		}
	}
	return
}