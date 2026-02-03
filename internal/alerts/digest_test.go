package alerts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewDigestScheduler(t *testing.T) {
	generateFn := func() (*DigestData, error) {
		return &DigestData{}, nil
	}
	sendFn := func(*DigestData) error {
		return nil
	}

	scheduler, err := NewDigestScheduler("UTC", "09:00", generateFn, sendFn)
	assert.NoError(t, err)
	assert.NotNil(t, scheduler)
	assert.Equal(t, "09:00", scheduler.digestTime)
	assert.Equal(t, "UTC", scheduler.timezone.String())
}

func TestNewDigestSchedulerInvalidTimezone(t *testing.T) {
	generateFn := func() (*DigestData, error) {
		return &DigestData{}, nil
	}
	sendFn := func(*DigestData) error {
		return nil
	}

	// Invalid timezone falls back to UTC
	scheduler, err := NewDigestScheduler("Invalid/Timezone", "09:00", generateFn, sendFn)
	assert.NoError(t, err)
	assert.NotNil(t, scheduler)
	assert.Equal(t, "UTC", scheduler.timezone.String())
}

func TestNewDigestSchedulerDefaultTime(t *testing.T) {
	generateFn := func() (*DigestData, error) {
		return &DigestData{}, nil
	}
	sendFn := func(*DigestData) error {
		return nil
	}

	scheduler, err := NewDigestScheduler("UTC", "", generateFn, sendFn)
	assert.NoError(t, err)
	assert.NotNil(t, scheduler)
	assert.Equal(t, "09:00", scheduler.digestTime)
}

func TestDigestSchedulerStartStop(t *testing.T) {
	generateFn := func() (*DigestData, error) {
		return &DigestData{}, nil
	}
	sendFn := func(*DigestData) error {
		return nil
	}

	scheduler, _ := NewDigestScheduler("UTC", "09:00", generateFn, sendFn)

	// Should not be running initially
	assert.False(t, scheduler.IsRunning())

	// Start
	scheduler.Start()
	assert.True(t, scheduler.IsRunning())

	// Stop
	scheduler.Stop()
	assert.False(t, scheduler.IsRunning())
}

func TestDigestSchedulerDoubleStart(t *testing.T) {
	generateFn := func() (*DigestData, error) {
		return &DigestData{}, nil
	}
	sendFn := func(*DigestData) error {
		return nil
	}

	scheduler, _ := NewDigestScheduler("UTC", "09:00", generateFn, sendFn)

	scheduler.Start()
	scheduler.Start() // Should not panic or create extra goroutines

	assert.True(t, scheduler.IsRunning())
	scheduler.Stop()
}

func TestGenerateDigest(t *testing.T) {
	alerts := []Alert{
		{Severity: SeverityCritical, Timestamp: time.Now()},
		{Severity: SeverityCritical, Timestamp: time.Now()},
		{Severity: SeverityWarning, Timestamp: time.Now()},
	}

	accountUsages := []AccountUsage{
		{AccountID: "acc1", Provider: "openai", UsagePercent: 90.0},
		{AccountID: "acc2", Provider: "anthropic", UsagePercent: 70.0},
		{AccountID: "acc3", Provider: "gemini", UsagePercent: 80.0},
	}

	digest := GenerateDigest(alerts, accountUsages)

	assert.NotNil(t, digest)
	assert.Equal(t, 2, len(digest.Alerts)) // Critical and Warning
	assert.Equal(t, 3, len(digest.TopAccounts))

	// Should be sorted by severity (critical first)
	assert.Equal(t, SeverityCritical, digest.Alerts[0].Severity)
	assert.Equal(t, 2, digest.Alerts[0].Count)

	// Should be sorted by usage
	assert.Equal(t, "acc1", digest.TopAccounts[0].AccountID)
	assert.Equal(t, 90.0, digest.TopAccounts[0].UsagePercent)
}

func TestGenerateDigestEmpty(t *testing.T) {
	digest := GenerateDigest([]Alert{}, []AccountUsage{})

	assert.NotNil(t, digest)
	assert.Equal(t, 0, len(digest.Alerts))
	assert.Equal(t, 0, len(digest.TopAccounts))
}

func TestGenerateDigestTopAccountsLimit(t *testing.T) {
	accountUsages := []AccountUsage{
		{AccountID: "acc1", UsagePercent: 90.0},
		{AccountID: "acc2", UsagePercent: 80.0},
		{AccountID: "acc3", UsagePercent: 70.0},
		{AccountID: "acc4", UsagePercent: 60.0},
		{AccountID: "acc5", UsagePercent: 50.0},
		{AccountID: "acc6", UsagePercent: 40.0},
	}

	digest := GenerateDigest([]Alert{}, accountUsages)

	// Should limit to top 5
	assert.Equal(t, 5, len(digest.TopAccounts))
}

func TestFormatDigest(t *testing.T) {
	digest := &DigestData{
		Date: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Alerts: []AlertSummary{
			{Severity: SeverityCritical, Count: 2},
			{Severity: SeverityWarning, Count: 5},
		},
		TopAccounts: []AccountUsage{
			{AccountID: "acc1", Provider: "openai", UsagePercent: 90.0},
			{AccountID: "acc2", Provider: "anthropic", UsagePercent: 75.0},
		},
	}

	result := FormatDigest(digest)

	assert.Contains(t, result, "Daily Digest")
	assert.Contains(t, result, "2024-01-15")
	assert.Contains(t, result, "acc1")
	assert.Contains(t, result, "acc2")
	assert.Contains(t, result, "90.0%")
}

func TestFormatDigestNoAlerts(t *testing.T) {
	digest := &DigestData{
		Date:        time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		Alerts:      []AlertSummary{},
		TopAccounts: []AccountUsage{},
	}

	result := FormatDigest(digest)

	assert.Contains(t, result, "Daily Digest")
	// Should not contain alert section
	assert.NotContains(t, result, "Alert Summary")
}

func TestCalculateNextDelay(t *testing.T) {
	generateFn := func() (*DigestData, error) {
		return &DigestData{}, nil
	}
	sendFn := func(*DigestData) error {
		return nil
	}

	// Test with time in the future
	scheduler, _ := NewDigestScheduler("UTC", "23:59", generateFn, sendFn)
	delay := scheduler.calculateNextDelay()
	assert.Greater(t, delay, time.Duration(0))
	assert.Less(t, delay, 24*time.Hour)
}

func TestCalculateNextDelayPast(t *testing.T) {
	generateFn := func() (*DigestData, error) {
		return &DigestData{}, nil
	}
	sendFn := func(*DigestData) error {
		return nil
	}

	// Test with time in the past (should schedule for tomorrow)
	scheduler, _ := NewDigestScheduler("UTC", "00:00", generateFn, sendFn)
	delay := scheduler.calculateNextDelay()
	// Should be less than 24 hours but more than current time until midnight
	assert.Greater(t, delay, time.Duration(0))
	assert.Less(t, delay, 24*time.Hour)
}
