package logging

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	// CorrelationIDKey is the context key for correlation ID
	CorrelationIDKey contextKey = "correlation_id"
)

// WithCorrelationID adds a correlation ID to the context
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// GetCorrelationID retrieves the correlation ID from the context
// Returns empty string if not set
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return id
	}
	return ""
}

// GenerateCorrelationID generates a new UUID-based correlation ID
func GenerateCorrelationID() string {
	return uuid.New().String()
}

// MustGetCorrelationID retrieves the correlation ID from the context
// If not set, generates a new one and returns it
func MustGetCorrelationID(ctx context.Context) string {
	if id := GetCorrelationID(ctx); id != "" {
		return id
	}
	newID := GenerateCorrelationID()
	return newID
}
