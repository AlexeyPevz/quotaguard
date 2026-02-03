package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsRecordingAndHandler(t *testing.T) {
	m := NewMetrics("test")

	m.RecordRequestLatency("/health", "GET", "200", 0.01)
	m.RecordQuotaUtilization("acc", "openai", "rpm", 75.5)
	m.RecordRouterDecision("balanced", "selected", "openai")
	m.RecordReservation("create", "success")
	m.RecordCollector("poll", "success", "cron")
	m.RecordError("timeout", "/health", "GET")
	m.SetAccountHealth("acc", "openai", true)
	m.RecordHTTPRequest("/health", "GET", "200")
	m.IncHTTPRequestsInFlight()
	m.DecHTTPRequestsInFlight()
	m.RecordLimiterAcquire("success")
	m.RecordLimiterRelease()
	m.RecordLimiterWaitDuration("success", 0.02)
	m.SetLimiterTokensAvailable("acc", 5)
	m.SetLimiterCapacity("acc", 10)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "test_request_latency_seconds") {
		t.Fatalf("expected metrics output to contain request latency metric")
	}

	if _, err := m.registry.Gather(); err != nil {
		t.Fatalf("expected gather to succeed: %v", err)
	}
}
