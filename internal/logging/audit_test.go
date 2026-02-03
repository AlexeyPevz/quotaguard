package logging

import (
	"strings"
	"testing"
)

func TestAuditEventLifecycle(t *testing.T) {
	event := NewAuditEvent(AuthSuccess, "login", StatusSuccess).
		WithUserID("user").
		WithIPAddress("127.0.0.1").
		WithResource("/login").
		WithSeverity(SeverityInfo).
		WithDetails(map[string]interface{}{"k": "v"})

	if event.UserID != "user" || event.IPAddress != "127.0.0.1" {
		t.Fatalf("expected user and ip to be set")
	}
	if event.Resource != "/login" || event.Severity != SeverityInfo {
		t.Fatalf("expected resource and severity to be set")
	}

	event.WithError("boom")
	if event.Status != StatusFailure {
		t.Fatalf("expected status to be failure")
	}
	if event.ErrorMessage != "boom" {
		t.Fatalf("expected error message")
	}

	jsonStr := event.ToJSON()
	if !strings.Contains(jsonStr, "login") {
		t.Fatalf("expected json output to contain action")
	}

	parsed, err := ParseAuditEvent(jsonStr)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if parsed.Action != event.Action {
		t.Fatalf("expected parsed action to match")
	}
}

func TestAuditEventJSONErrors(t *testing.T) {
	event := NewAuditEvent(APIAccess, "call", StatusSuccess)
	event.Details = map[string]interface{}{"bad": func() {}}
	jsonStr := event.ToJSON()
	if !strings.Contains(jsonStr, "failed to marshal audit event") {
		t.Fatalf("expected marshal failure message")
	}

	if _, err := ParseAuditEvent("{invalid json"); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestEventTypeFromString(t *testing.T) {
	if EventTypeFromString(string(AuthFailure)) != AuthFailure {
		t.Fatalf("expected auth failure event type")
	}
	if EventTypeFromString("unknown") != APIAccess {
		t.Fatalf("expected fallback event type")
	}
}

func TestWithErrorDefaultsSeverity(t *testing.T) {
	event := NewAuditEvent(ConfigChange, "update", StatusSuccess)
	event.Severity = ""
	event.WithError("bad")
	if event.Severity != SeverityError {
		t.Fatalf("expected severity error")
	}
}
