package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// EventType represents the type of audit event
type EventType string

const (
	EventTypeAuthentication EventType = "authentication"
	EventTypeAuthorization  EventType = "authorization"
	EventTypeConnection     EventType = "connection"
	EventTypeConfiguration  EventType = "configuration"
	EventTypeDataTransfer   EventType = "data_transfer"
	EventTypeError          EventType = "error"
	EventTypeSystem         EventType = "system"
)

// EventLevel represents the severity level
type EventLevel string

const (
	LevelInfo     EventLevel = "info"
	LevelWarning  EventLevel = "warning"
	LevelError    EventLevel = "error"
	LevelCritical EventLevel = "critical"
)

// AuditEvent represents a single audit event
type AuditEvent struct {
	Timestamp   time.Time              `json:"timestamp"`
	EventType   EventType              `json:"event_type"`
	Level       EventLevel             `json:"level"`
	Username    string                 `json:"username,omitempty"`
	SourceIP    string                 `json:"source_ip,omitempty"`
	Action      string                 `json:"action"`
	Resource    string                 `json:"resource,omitempty"`
	Result      string                 `json:"result"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	Duration    time.Duration          `json:"duration,omitempty"`
	ErrorCode   string                 `json:"error_code,omitempty"`
}

// AuditLogger manages audit logging
type AuditLogger struct {
	output      io.Writer
	buffer      []*AuditEvent
	bufferSize  int
	mu          sync.Mutex
	encoder     *json.Encoder
	file        *os.File
	rotateSize  int64
	currentSize int64
}

// AuditLoggerConfig configures the audit logger
type AuditLoggerConfig struct {
	OutputPath string
	BufferSize int
	RotateSize int64 // Size in bytes before rotation
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(config AuditLoggerConfig) (*AuditLogger, error) {
	var output io.Writer
	var file *os.File
	var err error

	if config.OutputPath == "" || config.OutputPath == "stdout" {
		output = os.Stdout
	} else {
		file, err = os.OpenFile(config.OutputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open audit log file: %w", err)
		}
		output = file

		// Get current file size
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to stat audit log file: %w", err)
		}
		config.RotateSize = info.Size()
	}

	if config.BufferSize == 0 {
		config.BufferSize = 100
	}

	logger := &AuditLogger{
		output:     output,
		buffer:     make([]*AuditEvent, 0, config.BufferSize),
		bufferSize: config.BufferSize,
		encoder:    json.NewEncoder(output),
		file:       file,
		rotateSize: config.RotateSize,
	}

	return logger, nil
}

// Log logs an audit event
func (al *AuditLogger) Log(event *AuditEvent) error {
	event.Timestamp = time.Now()

	al.mu.Lock()
	defer al.mu.Unlock()

	// Write to output
	if err := al.encoder.Encode(event); err != nil {
		return fmt.Errorf("failed to encode audit event: %w", err)
	}

	// Add to buffer
	al.buffer = append(al.buffer, event)
	if len(al.buffer) > al.bufferSize {
		al.buffer = al.buffer[1:]
	}

	// Update size and check rotation
	if al.file != nil {
		data, _ := json.Marshal(event)
		al.currentSize += int64(len(data)) + 1 // +1 for newline
		if al.rotateSize > 0 && al.currentSize >= al.rotateSize {
			al.rotate()
		}
	}

	return nil
}

// LogAuthentication logs an authentication event
func (al *AuditLogger) LogAuthentication(username, sourceIP, result, message string) error {
	return al.Log(&AuditEvent{
		EventType: EventTypeAuthentication,
		Level:     LevelInfo,
		Username:  username,
		SourceIP:  sourceIP,
		Action:    "authenticate",
		Result:    result,
		Message:   message,
	})
}

// LogConnection logs a connection event
func (al *AuditLogger) LogConnection(username, sourceIP, action, result string) error {
	return al.Log(&AuditEvent{
		EventType: EventTypeConnection,
		Level:     LevelInfo,
		Username:  username,
		SourceIP:  sourceIP,
		Action:    action,
		Result:    result,
	})
}

// LogConfiguration logs a configuration change
func (al *AuditLogger) LogConfiguration(username, resource, action, result string, details map[string]interface{}) error {
	return al.Log(&AuditEvent{
		EventType: EventTypeConfiguration,
		Level:     LevelInfo,
		Username:  username,
		Action:    action,
		Resource:  resource,
		Result:    result,
		Details:   details,
	})
}

// LogError logs an error event
func (al *AuditLogger) LogError(action, message, errorCode string, details map[string]interface{}) error {
	return al.Log(&AuditEvent{
		EventType: EventTypeError,
		Level:     LevelError,
		Action:    action,
		Result:    "failure",
		Message:   message,
		ErrorCode: errorCode,
		Details:   details,
	})
}

// LogDataTransfer logs a data transfer event
func (al *AuditLogger) LogDataTransfer(username, sourceIP string, bytesTransferred int64, duration time.Duration) error {
	return al.Log(&AuditEvent{
		EventType: EventTypeDataTransfer,
		Level:     LevelInfo,
		Username:  username,
		SourceIP:  sourceIP,
		Action:    "data_transfer",
		Result:    "success",
		Duration:  duration,
		Details: map[string]interface{}{
			"bytes_transferred": bytesTransferred,
		},
	})
}

// GetRecentEvents returns recent events from the buffer
func (al *AuditLogger) GetRecentEvents(count int) []*AuditEvent {
	al.mu.Lock()
	defer al.mu.Unlock()

	if count > len(al.buffer) {
		count = len(al.buffer)
	}

	events := make([]*AuditEvent, count)
	start := len(al.buffer) - count
	copy(events, al.buffer[start:])

	return events
}

// rotate rotates the audit log file
func (al *AuditLogger) rotate() error {
	if al.file == nil {
		return nil
	}

	// Close current file
	if err := al.file.Close(); err != nil {
		return err
	}

	// Rename current file with timestamp
	oldPath := al.file.Name()
	newPath := fmt.Sprintf("%s.%s", oldPath, time.Now().Format("20060102-150405"))
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}

	// Open new file
	file, err := os.OpenFile(oldPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	al.file = file
	al.output = file
	al.encoder = json.NewEncoder(file)
	al.currentSize = 0

	return nil
}

// Close closes the audit logger
func (al *AuditLogger) Close() error {
	al.mu.Lock()
	defer al.mu.Unlock()

	if al.file != nil {
		return al.file.Close()
	}
	return nil
}

// Flush ensures all buffered data is written
func (al *AuditLogger) Flush() error {
	al.mu.Lock()
	defer al.mu.Unlock()

	if al.file != nil {
		return al.file.Sync()
	}
	return nil
}

// SearchEvents searches for events matching criteria
func (al *AuditLogger) SearchEvents(eventType EventType, username string, startTime, endTime time.Time) []*AuditEvent {
	al.mu.Lock()
	defer al.mu.Unlock()

	results := make([]*AuditEvent, 0)
	for _, event := range al.buffer {
		if eventType != "" && event.EventType != eventType {
			continue
		}
		if username != "" && event.Username != username {
			continue
		}
		if !startTime.IsZero() && event.Timestamp.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && event.Timestamp.After(endTime) {
			continue
		}
		results = append(results, event)
	}

	return results
}

// GetStatistics returns audit statistics
func (al *AuditLogger) GetStatistics() map[string]interface{} {
	al.mu.Lock()
	defer al.mu.Unlock()

	stats := map[string]interface{}{
		"total_events":  len(al.buffer),
		"buffer_size":   al.bufferSize,
		"current_size":  al.currentSize,
	}

	// Count by event type
	typeCounts := make(map[EventType]int)
	levelCounts := make(map[EventLevel]int)

	for _, event := range al.buffer {
		typeCounts[event.EventType]++
		levelCounts[event.Level]++
	}

	stats["event_types"] = typeCounts
	stats["event_levels"] = levelCounts

	return stats
}
