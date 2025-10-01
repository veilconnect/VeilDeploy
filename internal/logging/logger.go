package logging

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func ParseLevel(input string) Level {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

type Logger struct {
	mu    sync.Mutex
	level Level
	base  map[string]interface{}
	log   *log.Logger
}

func New(level Level, output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}
	return &Logger{
		level: level,
		base:  map[string]interface{}{},
		log:   log.New(output, "", 0),
	}
}

func (l *Logger) With(fields map[string]interface{}) *Logger {
	child := &Logger{
		level: l.level,
		log:   l.log,
		base:  make(map[string]interface{}, len(l.base)+len(fields)),
	}
	for k, v := range l.base {
		child.base[k] = v
	}
	for k, v := range fields {
		child.base[k] = v
	}
	return child
}

func (l *Logger) logf(level Level, msg string, fields map[string]interface{}) {
	if level < l.level {
		return
	}
	payload := make(map[string]interface{}, len(l.base)+len(fields)+3)
	for k, v := range l.base {
		payload[k] = v
	}
	for k, v := range fields {
		payload[k] = v
	}
	payload["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	payload["level"] = levelString(level)
	payload["message"] = msg
	data, err := json.Marshal(payload)
	if err != nil {
		l.mu.Lock()
		l.log.Printf("{\"level\":\"error\",\"message\":\"log marshal failed\",\"error\":%q}", err.Error())
		l.mu.Unlock()
		return
	}
	l.mu.Lock()
	l.log.Println(string(data))
	l.mu.Unlock()
}

func levelString(level Level) string {
	switch level {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	l.logf(LevelDebug, msg, fields)
}

func (l *Logger) Info(msg string, fields map[string]interface{}) {
	l.logf(LevelInfo, msg, fields)
}

func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	l.logf(LevelWarn, msg, fields)
}

func (l *Logger) Error(msg string, fields map[string]interface{}) {
	l.logf(LevelError, msg, fields)
}

func (l *Logger) SetLevel(level Level) {
	l.level = level
}
