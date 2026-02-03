package models

import "time"

// QuotaEvent represents a change in quota status
type QuotaEvent struct {
	AccountID string
	Dimension Dimension
	OldValue  int64
	NewValue  int64
	Timestamp time.Time
}
