package logging

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AuditEventType represents the type of audit event
type AuditEventType string

const (
	// Authentication events
	AuthSuccess AuditEventType = "AUTH_SUCCESS"
	AuthFailure AuditEventType = "AUTH_FAILURE"

	// Configuration events
	ConfigChange AuditEventType = "CONFIG_CHANGE"

	// Account events
	AccountCreate AuditEventType = "ACCOUNT_CREATE"
	AccountDelete AuditEventType = "ACCOUNT_DELETE"

	// Quota events
	QuotaAlert AuditEventType = "QUOTA_ALERT"

	// API access events
	APIAccess AuditEventType = "API_ACCESS"

	// Admin actions
	AdminAction AuditEventType = "ADMIN_ACTION"
)

// AuditSeverity represents the severity level of an audit event
type AuditSeverity string

const (
	SeverityInfo     AuditSeverity = "info"
	SeverityWarning  AuditSeverity = "warning"
	SeverityError    AuditSeverity = "error"
	SeverityCritical AuditSeverity = "critical"
)

// AuditStatus represents the status of an audited action
type AuditStatus string

const (
	StatusSuccess AuditStatus = "success"
	StatusFailure AuditStatus = "failure"
)

// AuditEvent represents a security/operational audit event
type AuditEvent struct {
	ID           string                 `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	EventType    AuditEventType         `json:"event_type"`
	Severity     AuditSeverity          `json:"severity"`
	UserID       string                 `json:"user_id,omitempty"`
	IPAddress    string                 `json:"ip_address"`
	Action       string                 `json:"action"`
	Resource     string                 `json:"resource"`
	Status       AuditStatus            `json:"status"`
	Details      map[string]interface{} `json:"details,omitempty"`
	ErrorMessage string                 `json:"error_message,omitempty"`
}

// NewAuditEvent creates a new audit event with a generated ID and timestamp
func NewAuditEvent(eventType AuditEventType, action string, status AuditStatus) *AuditEvent {
	return &AuditEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		Action:    action,
		Status:    status,
	}
}

// WithUserID sets the user ID for the audit event
func (e *AuditEvent) WithUserID(userID string) *AuditEvent {
	e.UserID = userID
	return e
}

// WithIPAddress sets the IP address for the audit event
func (e *AuditEvent) WithIPAddress(ipAddress string) *AuditEvent {
	e.IPAddress = ipAddress
	return e
}

// WithResource sets the resource for the audit event
func (e *AuditEvent) WithResource(resource string) *AuditEvent {
	e.Resource = resource
	return e
}

// WithSeverity sets the severity for the audit event
func (e *AuditEvent) WithSeverity(severity AuditSeverity) *AuditEvent {
	e.Severity = severity
	return e
}

// WithDetails sets the details map for the audit event
func (e *AuditEvent) WithDetails(details map[string]interface{}) *AuditEvent {
	e.Details = details
	return e
}

// WithError sets the error message for the audit event
func (e *AuditEvent) WithError(errorMessage string) *AuditEvent {
	e.ErrorMessage = errorMessage
	e.Status = StatusFailure
	if e.Severity == "" {
		e.Severity = SeverityError
	}
	return e
}

// ToJSON converts the audit event to a JSON string
func (e *AuditEvent) ToJSON() string {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to marshal audit event: %v"}`, err)
	}
	return string(data)
}

// ToJSONBytes converts the audit event to JSON bytes
func (e *AuditEvent) ToJSONBytes() ([]byte, error) {
	return json.Marshal(e)
}

// ParseAuditEvent parses a JSON string into an AuditEvent
func ParseAuditEvent(data string) (*AuditEvent, error) {
	var event AuditEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil, fmt.Errorf("failed to parse audit event: %w", err)
	}
	return &event, nil
}

// EventTypeFromString converts a string to AuditEventType
func EventTypeFromString(s string) AuditEventType {
	switch s {
	case string(AuthSuccess):
		return AuthSuccess
	case string(AuthFailure):
		return AuthFailure
	case string(ConfigChange):
		return ConfigChange
	case string(AccountCreate):
		return AccountCreate
	case string(AccountDelete):
		return AccountDelete
	case string(QuotaAlert):
		return QuotaAlert
	case string(APIAccess):
		return APIAccess
	case string(AdminAction):
		return AdminAction
	default:
		return APIAccess
	}
}
