package telegram

import (
	"strings"
	"testing"
)

func TestFormatQuotasClamp(t *testing.T) {
	msg := formatQuotas([]AccountQuota{
		{AccountID: "acc-1", Provider: "gemini", UsagePercent: 150},
		{AccountID: "acc-2", Provider: "openai", UsagePercent: -10},
	})
	if !strings.Contains(msg, "100%") {
		t.Fatalf("expected clamped 100%%, got: %s", msg)
	}
	if strings.Contains(msg, "-10%") {
		t.Fatalf("expected clamped 0%%, got: %s", msg)
	}
}
