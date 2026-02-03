package alerts

import (
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// Severity represents alert severity level
type Severity string

const (
	// SeverityInfo is for informational alerts
	SeverityInfo Severity = "info"
	// SeverityWarning is for warning alerts
	SeverityWarning Severity = "warning"
	// SeverityCritical is for critical alerts
	SeverityCritical Severity = "critical"
)

// AlertType represents the type of alert
type AlertType string

const (
	// AlertTypeThreshold is for threshold-based alerts
	AlertTypeThreshold AlertType = "threshold"
	// AlertTypeExhausted is for exhausted quota alerts
	AlertTypeExhausted AlertType = "exhausted"
	// AlertTypeError is for error alerts
	AlertTypeError AlertType = "error"
	// AlertTypeDailyDigest is for daily digest
	AlertTypeDailyDigest AlertType = "daily_digest"
)

// Alert represents an alert to be sent
type Alert struct {
	ID        string
	AccountID string
	Type      AlertType
	Severity  Severity
	Message   string
	Threshold float64
	Current   float64
	Timestamp time.Time
	Metadata  map[string]interface{}
}

// AlertKey creates a unique key for deduplication
func (a *Alert) AlertKey() string {
	return a.AccountID + ":" + string(a.Type) + ":" + string(a.Severity)
}

// AlertRecord represents a sent alert record for deduplication
type AlertRecord struct {
	AlertKey string
	SentAt   time.Time
	Count    int
}

// DigestData represents data for daily digest
type DigestData struct {
	Date          time.Time
	TotalRequests int64
	Switches      int
	Errors        int
	TopAccounts   []AccountUsage
	Alerts        []AlertSummary
}

// AccountUsage represents account usage statistics
type AccountUsage struct {
	AccountID    string
	Provider     string
	UsagePercent float64
	Requests     int64
}

// AlertSummary represents a summary of alerts
type AlertSummary struct {
	Severity Severity
	Count    int
	LastAt   time.Time
}

// ThresholdCheck represents a threshold check result
type ThresholdCheck struct {
	Account   models.Account
	Quota     models.QuotaInfo
	Threshold float64
	Exceeded  bool
}

// MuteState represents the mute state for alerts
type MuteState struct {
	Muted  bool
	Until  time.Time
	Reason string
}

// IsMuted checks if alerts are currently muted
func (m *MuteState) IsMuted() bool {
	if !m.Muted {
		return false
	}
	if time.Now().After(m.Until) {
		m.Muted = false
		return false
	}
	return true
}

// RemainingMuteTime returns the remaining mute duration
func (m *MuteState) RemainingMuteTime() time.Duration {
	if !m.IsMuted() {
		return 0
	}
	return time.Until(m.Until)
}
