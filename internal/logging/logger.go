package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// LogLevel represents the severity level of a log message
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelPanic LogLevel = "panic"
	LevelFatal LogLevel = "fatal"
)

// Logger provides structured JSON logging with correlation ID support
type Logger struct {
	mu      sync.Mutex
	output  io.Writer
	level   LogLevel
	service string
}

// LoggerOption is a function that configures a Logger
type LoggerOption func(*Logger)

// WithOutput sets the output writer for the logger
func WithOutput(w io.Writer) LoggerOption {
	return func(l *Logger) {
		l.output = w
	}
}

// WithLevel sets the minimum log level
func WithLevel(level LogLevel) LoggerOption {
	return func(l *Logger) {
		l.level = level
	}
}

// WithService sets the service name for logs
func WithService(service string) LoggerOption {
	return func(l *Logger) {
		l.service = service
	}
}

// NewLogger creates a new Logger with the specified options
func NewLogger(opts ...LoggerOption) *Logger {
	logger := &Logger{
		output:  os.Stdout,
		level:   LevelInfo,
		service: "quotaguard",
	}

	for _, opt := range opts {
		opt(logger)
	}

	return logger
}

// logEntry represents a structured log entry
type logEntry struct {
	Timestamp     string                 `json:"timestamp"`
	Level         LogLevel               `json:"level"`
	Service       string                 `json:"service"`
	Message       string                 `json:"message"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
}

// outputLog writes a log entry to the output
func (l *Logger) outputLog(entry logEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	entry.Service = l.service

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("failed to marshal log entry: %v", err)
		return
	}

	fmt.Fprintln(l.output, string(data))
}

// shouldLog checks if a log level should be logged
func (l *Logger) shouldLog(level LogLevel) bool {
	levels := map[LogLevel]int{
		LevelDebug: 0,
		LevelInfo:  1,
		LevelWarn:  2,
		LevelError: 3,
		LevelPanic: 4,
		LevelFatal: 5,
	}

	return levels[level] >= levels[l.level]
}

// log outputs a log message with the specified level and fields
func (l *Logger) log(level LogLevel, message string, correlationID string, fields map[string]interface{}) {
	if !l.shouldLog(level) {
		return
	}

	entry := logEntry{
		Level:         level,
		Message:       message,
		CorrelationID: correlationID,
		Fields:        fields,
	}

	l.outputLog(entry)

	// Handle panic level
	if level == LevelPanic {
		panic(message)
	}

	// Handle fatal level
	if level == LevelFatal {
		os.Exit(1)
	}
}

// Debug logs a debug message
func (l *Logger) Debug(message string, fields ...interface{}) {
	correlationID, fieldMap := parseFields(fields)
	l.log(LevelDebug, message, correlationID, fieldMap)
}

// Info logs an info message
func (l *Logger) Info(message string, fields ...interface{}) {
	correlationID, fieldMap := parseFields(fields)
	l.log(LevelInfo, message, correlationID, fieldMap)
}

// Warn logs a warning message
func (l *Logger) Warn(message string, fields ...interface{}) {
	correlationID, fieldMap := parseFields(fields)
	l.log(LevelWarn, message, correlationID, fieldMap)
}

// Error logs an error message
func (l *Logger) Error(message string, fields ...interface{}) {
	correlationID, fieldMap := parseFields(fields)
	l.log(LevelError, message, correlationID, fieldMap)
}

// Panic logs a panic message and panics
func (l *Logger) Panic(message string, fields ...interface{}) {
	correlationID, fieldMap := parseFields(fields)
	l.log(LevelPanic, message, correlationID, fieldMap)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(message string, fields ...interface{}) {
	correlationID, fieldMap := parseFields(fields)
	l.log(LevelFatal, message, correlationID, fieldMap)
}

// DebugWithContext logs a debug message with correlation ID from context
func (l *Logger) DebugWithContext(ctx context.Context, message string, fields ...interface{}) {
	correlationID := GetCorrelationID(ctx)
	_, fieldMap := parseFields(fields)
	l.log(LevelDebug, message, correlationID, fieldMap)
}

// InfoWithContext logs an info message with correlation ID from context
func (l *Logger) InfoWithContext(ctx context.Context, message string, fields ...interface{}) {
	correlationID := GetCorrelationID(ctx)
	_, fieldMap := parseFields(fields)
	l.log(LevelInfo, message, correlationID, fieldMap)
}

// WarnWithContext logs a warning message with correlation ID from context
func (l *Logger) WarnWithContext(ctx context.Context, message string, fields ...interface{}) {
	correlationID := GetCorrelationID(ctx)
	_, fieldMap := parseFields(fields)
	l.log(LevelWarn, message, correlationID, fieldMap)
}

// ErrorWithContext logs an error message with correlation ID from context
func (l *Logger) ErrorWithContext(ctx context.Context, message string, fields ...interface{}) {
	correlationID := GetCorrelationID(ctx)
	_, fieldMap := parseFields(fields)
	l.log(LevelError, message, correlationID, fieldMap)
}

// parseFields parses variable number of key-value pairs into a map
// Expected format: key1, value1, key2, value2, ...
func parseFields(fields []interface{}) (string, map[string]interface{}) {
	correlationID := ""
	fieldMap := make(map[string]interface{})

	for i := 0; i < len(fields); i++ {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}

		if key == "correlation_id" && i+1 < len(fields) {
			if id, ok := fields[i+1].(string); ok {
				correlationID = id
			}
		} else if i+1 < len(fields) {
			fieldMap[key] = fields[i+1]
		}
		i++ // Skip the value
	}

	return correlationID, fieldMap
}
