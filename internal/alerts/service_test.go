package alerts

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/telegram"
	"github.com/stretchr/testify/assert"
)

// MockBot is a mock implementation of TelegramBot
type MockBot struct {
	messages []string
	alerts   []telegram.Alert
	enabled  bool
	mu       sync.Mutex
}

func NewMockBot() *MockBot {
	return &MockBot{
		messages: make([]string, 0),
		alerts:   make([]telegram.Alert, 0),
		enabled:  true,
	}
}

func (m *MockBot) SendMessage(text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, text)
	return nil
}

func (m *MockBot) SendAlert(alert telegram.Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, alert)
	return nil
}

func (m *MockBot) IsEnabled() bool {
	return m.enabled
}

func (m *MockBot) GetMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.messages))
	copy(result, m.messages)
	return result
}

func (m *MockBot) GetAlerts() []telegram.Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]telegram.Alert, len(m.alerts))
	copy(result, m.alerts)
	return result
}

func TestNewService(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)
	assert.NotNil(t, service)
	assert.NotNil(t, service.dedup)
	assert.NotNil(t, service.throttler)
	assert.NotNil(t, service.digest)
	assert.NotNil(t, service.muteState)
}

func TestServiceStartStop(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
		ShutdownTimeout: 5 * time.Second,
	}

	service := NewService(config, bot)

	// Test start
	service.Start()
	assert.True(t, service.IsRunning())

	// Test stop
	err := service.Stop()
	assert.NoError(t, err)
	assert.False(t, service.IsRunning())
}

func TestCheckThresholds(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)

	accounts := []models.Account{
		{ID: "acc1", Provider: models.ProviderOpenAI, Tier: "tier1"},
		{ID: "acc2", Provider: models.ProviderAnthropic, Tier: "tier2"},
	}

	quotas := []models.QuotaInfo{
		{AccountID: "acc1", EffectiveRemainingPct: 10.0}, // 90% used
		{AccountID: "acc2", EffectiveRemainingPct: 50.0}, // 50% used
	}

	alerts := service.CheckThresholds(accounts, quotas)

	// Should have 1 alert for acc1 (90% > 85% threshold)
	assert.Len(t, alerts, 1)
	assert.Equal(t, "acc1", alerts[0].AccountID)
	assert.Equal(t, SeverityWarning, alerts[0].Severity)
}

func TestCheckThresholdsCritical(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)

	accounts := []models.Account{
		{ID: "acc1", Provider: models.ProviderOpenAI, Tier: "tier1"},
	}

	quotas := []models.QuotaInfo{
		{AccountID: "acc1", EffectiveRemainingPct: 3.0}, // 97% used
	}

	alerts := service.CheckThresholds(accounts, quotas)

	// Should have 1 critical alert for acc1 (97% > 95% threshold)
	assert.Len(t, alerts, 1)
	assert.Equal(t, "acc1", alerts[0].AccountID)
	assert.Equal(t, SeverityCritical, alerts[0].Severity)
}

func TestCheckThresholdsExhausted(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)

	accounts := []models.Account{
		{ID: "acc1", Provider: models.ProviderOpenAI, Tier: "tier1"},
	}

	quota := models.QuotaInfo{
		AccountID: "acc1",
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionTPM, Used: 1000, Limit: 1000},
		},
	}
	quota.UpdateEffective()

	quotas := []models.QuotaInfo{quota}

	alerts := service.CheckThresholds(accounts, quotas)

	// Should have 2 alerts: one for threshold (100% >= 95%) and one for exhausted
	assert.Len(t, alerts, 2)
	assert.Equal(t, "acc1", alerts[0].AccountID)
	// First alert is threshold alert
	assert.Equal(t, AlertTypeThreshold, alerts[0].Type)
	assert.Equal(t, SeverityCritical, alerts[0].Severity)
	// Second alert is exhausted alert
	assert.Equal(t, AlertTypeExhausted, alerts[1].Type)
	assert.Equal(t, SeverityCritical, alerts[1].Severity)
}

func TestCheckThresholdsDisabled(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         false,
	}

	service := NewService(config, bot)

	accounts := []models.Account{
		{ID: "acc1", Provider: models.ProviderOpenAI, Tier: "tier1"},
	}

	quotas := []models.QuotaInfo{
		{AccountID: "acc1", EffectiveRemainingPct: 10.0},
	}

	alerts := service.CheckThresholds(accounts, quotas)

	// Should return nil when disabled
	assert.Nil(t, alerts)
}

func TestProcessAlert(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
		ShutdownTimeout: 5 * time.Second,
	}

	service := NewService(config, bot)
	service.Start()
	defer func() {
		assert.NoError(t, service.Stop())
	}()

	alert := Alert{
		ID:        "test-1",
		AccountID: "acc1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	err := service.ProcessAlert(alert)
	assert.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check that alert was recorded in dedup
	assert.Equal(t, 1, service.GetDedupSize())
}

func TestProcessAlertDuplicate(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
		ShutdownTimeout: 5 * time.Second,
	}

	service := NewService(config, bot)
	service.Start()
	defer func() {
		assert.NoError(t, service.Stop())
	}()

	alert := Alert{
		ID:        "test-1",
		AccountID: "acc1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	// Process first alert
	err := service.ProcessAlert(alert)
	assert.NoError(t, err)

	// Process duplicate alert - should be silently dropped
	err = service.ProcessAlert(alert)
	assert.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Should still have only 1 record
	assert.Equal(t, 1, service.GetDedupSize())
}

func TestProcessAlertMuted(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)

	// Mute alerts
	service.MuteAlerts(1*time.Hour, "testing")
	assert.True(t, service.IsMuted())

	alert := Alert{
		ID:        "test-1",
		AccountID: "acc1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	err := service.ProcessAlert(alert)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "muted")
}

func TestMuteUnmute(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)

	// Initially not muted
	assert.False(t, service.IsMuted())

	// Mute for 1 hour
	service.MuteAlerts(1*time.Hour, "testing")
	assert.True(t, service.IsMuted())
	assert.Greater(t, service.GetMuteRemaining(), time.Duration(0))

	// Unmute
	service.UnmuteAlerts()
	assert.False(t, service.IsMuted())
	assert.Equal(t, time.Duration(0), service.GetMuteRemaining())
}

func TestShouldSendAlert(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)

	alert := Alert{
		ID:        "test-1",
		AccountID: "acc1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	// Should allow first alert
	allowed, retryAfter := service.ShouldSendAlert(alert)
	assert.True(t, allowed)
	assert.Equal(t, time.Duration(0), retryAfter)

	// Record the alert
	service.dedup.Record(alert.AlertKey())

	// Should not allow duplicate
	allowed, retryAfter = service.ShouldSendAlert(alert)
	assert.False(t, allowed)
	assert.Greater(t, retryAfter, time.Duration(0))
}

func TestShouldSendAlertMuted(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot)
	service.MuteAlerts(1*time.Hour, "testing")

	alert := Alert{
		ID:        "test-1",
		AccountID: "acc1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	allowed, retryAfter := service.ShouldSendAlert(alert)
	assert.False(t, allowed)
	assert.Greater(t, retryAfter, time.Duration(0))
}

func TestShouldSendAlertDisabled(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         false,
	}

	service := NewService(config, bot)

	alert := Alert{
		ID:        "test-1",
		AccountID: "acc1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	allowed, _ := service.ShouldSendAlert(alert)
	assert.False(t, allowed)
}

func TestProcessAlerts(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
		ShutdownTimeout: 5 * time.Second,
	}

	service := NewService(config, bot)
	service.Start()
	defer func() {
		assert.NoError(t, service.Stop())
	}()

	alerts := []Alert{
		{ID: "test-1", AccountID: "acc1", Type: AlertTypeThreshold, Severity: SeverityWarning, Message: "Alert 1", Timestamp: time.Now()},
		{ID: "test-2", AccountID: "acc2", Type: AlertTypeThreshold, Severity: SeverityCritical, Message: "Alert 2", Timestamp: time.Now()},
	}

	err := service.ProcessAlerts(alerts)
	assert.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check that both alerts were recorded
	assert.Equal(t, 2, service.GetDedupSize())
}

func TestSendDailyDigest(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:         []float64{85.0, 95.0},
		Debounce:           30 * time.Minute,
		DailyDigestTime:    "09:00",
		Timezone:           "UTC",
		Enabled:            true,
		DailyDigestEnabled: true,
	}

	service := NewService(config, bot)

	err := service.SendDailyDigest()
	assert.NoError(t, err)

	// Check that a message was sent
	messages := bot.GetMessages()
	assert.Len(t, messages, 1)
	assert.Contains(t, messages[0], "Daily Digest")
}

func TestSendDailyDigestDisabled(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:         []float64{85.0, 95.0},
		Debounce:           30 * time.Minute,
		DailyDigestTime:    "09:00",
		Timezone:           "UTC",
		Enabled:            true,
		DailyDigestEnabled: false,
	}

	service := NewService(config, bot)

	err := service.SendDailyDigest()
	assert.NoError(t, err)

	// Check that no message was sent
	messages := bot.GetMessages()
	assert.Len(t, messages, 0)
}

func TestGracefulShutdown(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        30 * time.Minute,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
		ShutdownTimeout: 5 * time.Second,
	}

	service := NewService(config, bot)
	service.Start()

	// Add some pending alerts
	for i := 0; i < 5; i++ {
		alert := Alert{
			ID:        fmt.Sprintf("pending-%d", i),
			AccountID: "acc1",
			Type:      AlertTypeThreshold,
			Severity:  SeverityWarning,
			Message:   fmt.Sprintf("Pending alert %d", i),
			Timestamp: time.Now(),
		}
		service.pendingChan <- alert
	}

	// Stop should flush pending alerts
	err := service.Stop()
	assert.NoError(t, err)
}

func TestDedupCleanup(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:      []float64{85.0, 95.0},
		Debounce:        100 * time.Millisecond,
		DailyDigestTime: "09:00",
		Timezone:        "UTC",
		Enabled:         true,
	}

	service := NewService(config, bot, WithDedupWindow(100*time.Millisecond))

	alert := Alert{
		ID:        "test-1",
		AccountID: "acc1",
		Type:      AlertTypeThreshold,
		Severity:  SeverityWarning,
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	// Record alert
	service.dedup.Record(alert.AlertKey())
	assert.Equal(t, 1, service.GetDedupSize())

	// Wait for dedup window to expire
	time.Sleep(200 * time.Millisecond)

	// Cleanup
	service.dedup.Cleanup()
	assert.Equal(t, 0, service.GetDedupSize())
}

func TestThrottler(t *testing.T) {
	bot := NewMockBot()
	config := Config{
		Thresholds:         []float64{85.0, 95.0},
		Debounce:           30 * time.Minute,
		DailyDigestTime:    "09:00",
		Timezone:           "UTC",
		Enabled:            true,
		RateLimitPerMinute: 60, // 1 per second
	}

	service := NewService(config, bot)

	// Should have full tokens initially
	tokens := service.GetThrottlerTokens()
	assert.GreaterOrEqual(t, tokens, float64(50))

	// Use some tokens
	for i := 0; i < 10; i++ {
		service.throttler.Allow()
	}

	// Should have fewer tokens
	tokens = service.GetThrottlerTokens()
	assert.Less(t, tokens, float64(60))
}

func TestGenerateAlertID(t *testing.T) {
	id1 := generateAlertID()
	id2 := generateAlertID()

	// IDs should be unique
	assert.NotEqual(t, id1, id2)

	// Should start with "alert-"
	assert.True(t, len(id1) > 6)
}
