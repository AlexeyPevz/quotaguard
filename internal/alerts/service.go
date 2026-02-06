package alerts

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/telegram"
)

// TelegramBot interface for Telegram bot operations
type TelegramBot interface {
	SendMessage(text string) error
	SendAlert(alert telegram.Alert) error
	IsEnabled() bool
}

// Config represents alert service configuration
type Config struct {
	Thresholds         []float64
	Debounce           time.Duration
	DailyDigestTime    string
	Timezone           string
	RateLimitPerMinute int
	DailyDigestEnabled bool
	Enabled            bool
	ShutdownTimeout    time.Duration
	MuteDuration       time.Duration
}

// Service manages alerts and notifications
type Service struct {
	config    Config
	bot       TelegramBot
	dedup     *DedupStore
	throttler *Throttler
	digest    *DigestScheduler
	muteState *MuteState

	// Channels
	alertChan   chan Alert
	pendingChan chan Alert

	// State
	mu          sync.RWMutex
	running     bool
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	dedupWindow time.Duration
}

// ServiceOption is a functional option for Service
type ServiceOption func(*Service)

// WithDedupWindow sets the deduplication window
func WithDedupWindow(window time.Duration) ServiceOption {
	return func(s *Service) {
		s.dedupWindow = window
	}
}

// NewService creates a new alert service
func NewService(config Config, bot TelegramBot, opts ...ServiceOption) *Service {
	// Set defaults for config if not provided
	if config.Thresholds == nil {
		config.Thresholds = []float64{85.0, 95.0}
	}
	if config.Debounce == 0 {
		config.Debounce = 30 * time.Minute
	}
	if config.DailyDigestTime == "" {
		config.DailyDigestTime = "09:00"
	}
	if config.Timezone == "" {
		config.Timezone = "UTC"
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 25 * time.Second
	}

	s := &Service{
		config:      config,
		bot:         bot,
		alertChan:   make(chan Alert, 100),
		pendingChan: make(chan Alert, 1000),
		dedupWindow: 30 * time.Minute,
	}

	for _, opt := range opts {
		opt(s)
	}

	// Initialize dedup store
	s.dedup = NewDedupStore(s.dedupWindow)

	// Initialize throttler
	s.throttler = NewThrottler(s.config.RateLimitPerMinute, s.config.RateLimitPerMinute)

	// Initialize mute state
	s.muteState = &MuteState{}

	// Initialize digest scheduler
	var err error
	s.digest, err = NewDigestScheduler(
		s.config.Timezone,
		s.config.DailyDigestTime,
		s.generateDigest,
		s.sendDigest,
	)
	if err != nil {
		// Fall back to defaults if timezone is invalid
		s.digest, _ = NewDigestScheduler("UTC", "09:00", s.generateDigest, s.sendDigest)
	}

	return s
}

// Start starts the alert service
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	s.running = true
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// Start processing goroutines
	s.wg.Add(2)
	go s.processAlerts()
	go s.cleanupLoop()

	// Start digest scheduler if enabled
	if s.config.DailyDigestEnabled {
		s.digest.Start()
	}
}

// Stop gracefully stops the alert service
func (s *Service) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	// Signal shutdown
	if s.cancel != nil {
		s.cancel()
	}

	// Stop digest scheduler
	if s.digest != nil {
		s.digest.Stop()
	}

	// Flush pending alerts
	s.flushPendingAlerts()

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(s.config.ShutdownTimeout):
		return fmt.Errorf("timeout waiting for alert service to stop")
	}
}

// IsRunning returns whether the service is running
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// UpdateThresholds replaces the active thresholds for alerting.
func (s *Service) UpdateThresholds(thresholds []float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Thresholds = thresholds
}

// CheckThresholds checks thresholds for all accounts
func (s *Service) CheckThresholds(accounts []models.Account, quotas []models.QuotaInfo) []Alert {
	if !s.config.Enabled {
		return nil
	}

	var alerts []Alert

	for _, account := range accounts {
		// Find quota for this account
		var quota *models.QuotaInfo
		for i := range quotas {
			if quotas[i].AccountID == account.ID {
				quota = &quotas[i]
				break
			}
		}

		if quota == nil {
			continue
		}

		// Check each threshold (thresholds are usage percent)
		usedPercent := 100.0 - quota.EffectiveRemainingPct
		var highestExceededThreshold float64
		var maxThreshold float64
		for _, threshold := range s.config.Thresholds {
			if threshold > maxThreshold {
				maxThreshold = threshold
			}
			if usedPercent >= threshold && threshold > highestExceededThreshold {
				highestExceededThreshold = threshold
			}
		}

		if highestExceededThreshold > 0 {
			severity := SeverityWarning
			if usedPercent >= maxThreshold {
				severity = SeverityCritical
			}

			dimLabel := ""
			if quota.CriticalDimension != nil {
				if quota.CriticalDimension.Name != "" {
					dimLabel = quota.CriticalDimension.Name
				} else if quota.CriticalDimension.Type != "" {
					dimLabel = string(quota.CriticalDimension.Type)
				}
			}
			resetAt := nearestResetAt(quota)
			resetText := ""
			if resetAt != nil {
				resetText = fmt.Sprintf(" Next reset: %s", resetAt.Format("2006-01-02 15:04:05"))
			}
			dimText := ""
			if dimLabel != "" {
				dimText = fmt.Sprintf(" Critical dimension: %s.", dimLabel)
			}

			alert := Alert{
				ID:        generateAlertID(),
				AccountID: account.ID,
				Type:      AlertTypeThreshold,
				Severity:  severity,
				Message: fmt.Sprintf(
					"Quota remaining %.1f%% (used %.1f%%, threshold: %.1f%%).%s%s",
					quota.EffectiveRemainingPct,
					usedPercent,
					highestExceededThreshold,
					dimText,
					resetText,
				),
				Threshold: highestExceededThreshold,
				Current:   usedPercent,
				Timestamp: time.Now(),
				Metadata: map[string]interface{}{
					"provider": string(account.Provider),
					"tier":     account.Tier,
				},
			}

			alerts = append(alerts, alert)
		}

		// Check if quota is exhausted
		if quota.IsExhausted() {
			resetAt := nearestResetAt(quota)
			resetText := ""
			if resetAt != nil {
				resetText = fmt.Sprintf(" Next reset: %s", resetAt.Format("2006-01-02 15:04:05"))
			}
			alert := Alert{
				ID:        generateAlertID(),
				AccountID: account.ID,
				Type:      AlertTypeExhausted,
				Severity:  SeverityCritical,
				Message:   "Quota exhausted." + resetText,
				Timestamp: time.Now(),
				Metadata: map[string]interface{}{
					"provider": string(account.Provider),
					"tier":     account.Tier,
				},
			}
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

func nearestResetAt(quota *models.QuotaInfo) *time.Time {
	if quota == nil {
		return nil
	}
	var earliest *time.Time
	for _, dim := range quota.Dimensions {
		if dim.ResetAt == nil {
			continue
		}
		if earliest == nil || dim.ResetAt.Before(*earliest) {
			t := *dim.ResetAt
			earliest = &t
		}
	}
	return earliest
}

// ProcessAlert processes an alert with deduplication
func (s *Service) ProcessAlert(alert Alert) error {
	if !s.config.Enabled {
		return nil
	}

	// Check if muted
	if s.muteState.IsMuted() {
		return fmt.Errorf("alerts are muted for %v", s.muteState.RemainingMuteTime())
	}

	// Check deduplication
	key := alert.AlertKey()
	if s.dedup.IsDuplicate(key) {
		return nil // Silently drop duplicate
	}

	// Check rate limiting
	if !s.throttler.Allow() {
		// Queue for later
		select {
		case s.pendingChan <- alert:
			return nil
		default:
			return fmt.Errorf("alert queue is full")
		}
	}

	// Send to processing channel
	select {
	case s.alertChan <- alert:
		s.dedup.Record(key)
		return nil
	default:
		return fmt.Errorf("alert channel is full")
	}
}

// ProcessAlerts processes multiple alerts
func (s *Service) ProcessAlerts(alerts []Alert) error {
	for _, alert := range alerts {
		if err := s.ProcessAlert(alert); err != nil {
			return err
		}
	}
	return nil
}

// ShouldSendAlert checks if an alert should be sent (rate limiting and debounce)
func (s *Service) ShouldSendAlert(alert Alert) (bool, time.Duration) {
	if !s.config.Enabled {
		return false, 0
	}

	// Check mute state
	if s.muteState.IsMuted() {
		return false, s.muteState.RemainingMuteTime()
	}

	// Check deduplication
	key := alert.AlertKey()
	if s.dedup.IsDuplicate(key) {
		record := s.dedup.GetRecord(key)
		if record != nil {
			retryAfter := s.dedup.window - time.Since(record.SentAt)
			if retryAfter > 0 {
				return false, retryAfter
			}
		}
		return false, s.config.Debounce
	}

	// Check rate limiting
	if !s.throttler.Allow() {
		return false, s.throttler.GetRetryAfter()
	}

	return true, 0
}

// SendDailyDigest sends the daily digest immediately
func (s *Service) SendDailyDigest() error {
	if !s.config.Enabled || !s.config.DailyDigestEnabled {
		return nil
	}

	digest, err := s.generateDigest()
	if err != nil {
		return err
	}

	return s.sendDigest(digest)
}

// ScheduleDaily schedules daily digest at the specified time
func (s *Service) ScheduleDaily() {
	if s.digest != nil && s.config.DailyDigestEnabled {
		s.digest.Start()
	}
}

// MuteAlerts mutes alerts for the specified duration
func (s *Service) MuteAlerts(duration time.Duration, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.muteState.Muted = true
	s.muteState.Until = time.Now().Add(duration)
	s.muteState.Reason = reason
}

// UnmuteAlerts unmutes alerts
func (s *Service) UnmuteAlerts() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.muteState.Muted = false
	s.muteState.Until = time.Time{}
	s.muteState.Reason = ""
}

// IsMuted returns whether alerts are muted
func (s *Service) IsMuted() bool {
	return s.muteState.IsMuted()
}

// GetMuteRemaining returns the remaining mute duration
func (s *Service) GetMuteRemaining() time.Duration {
	return s.muteState.RemainingMuteTime()
}

// processAlerts processes alerts from the channel
func (s *Service) processAlerts() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case alert := <-s.alertChan:
			s.sendAlert(alert)
		}
	}
}

// cleanupLoop runs periodic cleanup tasks
func (s *Service) cleanupLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.dedup.Cleanup()
		}
	}
}

// sendAlert sends an alert via the bot
func (s *Service) sendAlert(alert Alert) {
	if s.bot == nil {
		return
	}

	tgAlert := telegram.Alert{
		ID:        alert.ID,
		Severity:  string(alert.Severity),
		Message:   alert.Message,
		AccountID: alert.AccountID,
		Timestamp: alert.Timestamp,
	}

	_ = s.bot.SendAlert(tgAlert)
}

// sendDigest sends a digest via the bot
func (s *Service) sendDigest(digest *DigestData) error {
	if s.bot == nil {
		return nil
	}

	// Convert to telegram digest
	tgDigest := &telegram.DailyDigest{
		Date:          digest.Date,
		TotalRequests: digest.TotalRequests,
		Switches:      digest.Switches,
		Errors:        digest.Errors,
	}

	for _, acc := range digest.TopAccounts {
		tgDigest.TopAccounts = append(tgDigest.TopAccounts, acc.AccountID)
	}

	// Format and send
	message := FormatDigest(digest)
	_ = s.bot.SendMessage(message)

	return nil
}

// generateDigest generates the daily digest
func (s *Service) generateDigest() (*DigestData, error) {
	// This would normally query actual data from the system
	// For now, return an empty digest
	return &DigestData{
		Date:          time.Now(),
		TotalRequests: 0,
		Switches:      0,
		Errors:        0,
		TopAccounts:   []AccountUsage{},
		Alerts:        []AlertSummary{},
	}, nil
}

// flushPendingAlerts flushes any pending alerts before shutdown
func (s *Service) flushPendingAlerts() {
	// Process remaining pending alerts
	close(s.pendingChan)
	for alert := range s.pendingChan {
		s.sendAlert(alert)
	}
}

// generateAlertID generates a unique alert ID
func generateAlertID() string {
	return fmt.Sprintf("alert-%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
}

// GetDedupSize returns the current dedup store size
func (s *Service) GetDedupSize() int {
	return s.dedup.Size()
}

// GetThrottlerTokens returns the current token count
func (s *Service) GetThrottlerTokens() float64 {
	return s.throttler.GetTokens()
}
