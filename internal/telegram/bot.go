package telegram

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/store"
)

// Message represents a message sent by the bot
type Message struct {
	ID        int64
	ChatID    int64
	Text      string
	Timestamp time.Time
}

// BotAPI interface for Telegram bot operations (allows mocking in tests)
type BotAPI interface {
	SendMessage(chatID int64, text string) error
	GetUpdates() ([]Message, error)
}

// ParseModeSender allows sending messages with parse mode (HTML/MarkdownV2).
type ParseModeSender interface {
	SendMessageWithParseMode(chatID int64, text string, parseMode string) error
}

// State represents the FSM state for user conversations
type State string

const (
	StateIdle          State = "idle"
	StateWaitingMute   State = "waiting_mute"
	StateWaitingSwitch State = "waiting_switch"
	StateConfirming    State = "confirming"
	StateWaitingOAuth  State = "waiting_oauth_callback"
)

// UserSession represents a user conversation session
type UserSession struct {
	UserID    int64
	State     State
	Data      map[string]interface{}
	UpdatedAt time.Time
}

// RateLimiter implements token bucket algorithm for rate limiting
type RateLimiter struct {
	rate       int // messages per minute
	bucketSize int // burst size
	tokens     float64
	lastUpdate time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(messagesPerMinute int) *RateLimiter {
	return &RateLimiter{
		rate:       messagesPerMinute,
		bucketSize: messagesPerMinute,
		tokens:     float64(messagesPerMinute),
		lastUpdate: time.Now(),
	}
}

// Allow checks if a message can be sent
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastUpdate).Minutes()
	rl.lastUpdate = now

	// Add tokens based on elapsed time
	rl.tokens += float64(rl.rate) * elapsed
	if rl.tokens > float64(rl.bucketSize) {
		rl.tokens = float64(rl.bucketSize)
	}

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// DedupLimiter prevents duplicate messages within a time window
type DedupLimiter struct {
	sent   map[string]time.Time
	window time.Duration
	mu     sync.RWMutex
}

// NewDedupLimiter creates a new deduplication limiter
func NewDedupLimiter(window time.Duration) *DedupLimiter {
	return &DedupLimiter{
		sent:   make(map[string]time.Time),
		window: window,
	}
}

// CanSend checks if a message can be sent (not a duplicate)
func (dl *DedupLimiter) CanSend(key string) bool {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	now := time.Now()
	if sentAt, exists := dl.sent[key]; exists {
		if now.Sub(sentAt) < dl.window {
			return false
		}
	}
	dl.sent[key] = now
	return true
}

// Cleanup removes old entries from the dedup limiter
func (dl *DedupLimiter) Cleanup() {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	now := time.Now()
	for key, sentAt := range dl.sent {
		if now.Sub(sentAt) > dl.window {
			delete(dl.sent, key)
		}
	}
}

// Alert represents an alert to be sent
type Alert struct {
	ID        string
	Severity  string
	Message   string
	AccountID string
	Timestamp time.Time
}

// BotOptions contains optional configuration for the bot
type BotOptions struct {
	RateLimiter  *RateLimiter
	DedupLimiter *DedupLimiter
	BotAPI       BotAPI
	Settings     store.SettingsStore
}

// Bot represents the Telegram bot for QuotaGuard
type Bot struct {
	botToken    string
	chatID      int64
	enabled     bool
	rateLimiter *RateLimiter
	dedup       *DedupLimiter
	sessions    map[int64]*UserSession
	sessionsMu  sync.RWMutex
	api         BotAPI
	settings    store.SettingsStore

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Channels
	msgChan   chan Message
	alertChan chan Alert

	autoImportEnabled bool

	// Callbacks for command handlers
	onGetStatus             func() (*SystemStatus, error)
	onGetQuotas             func() ([]AccountQuota, error)
	onGetAlerts             func() ([]ActiveAlert, error)
	onMuteAlerts            func(duration time.Duration) error
	onForceSwitch           func(accountID string) error
	onGetDailyDigest        func() (*DailyDigest, error)
	onUpdateThresholds      func(warning, switchVal, critical float64) error
	onUpdatePolicy          func(policy string) error
	onUpdateFallbackChains  func(chains map[string][]string) error
	onUpdateIgnoreEstimated func(ignore bool) error
	onGetRouterConfig       func() (*RouterConfig, error)
	onReloadConfig          func() error
	onExportConfig          func() (string, error)
	onImportAccounts        func(path string) (int, int, error)
	onGetAccounts           func() ([]AccountControl, error)
	onToggleAccount         func(accountID string, duration time.Duration, enable bool) error
	onGetAccountCheckConfig func() (*AccountCheckConfig, error)
	onSetAccountCheckConfig func(interval, timeout time.Duration) error
	onBuildLoginURL         func(provider string, chatID int64) (*LoginURLPayload, error)
	onCompleteOAuthLogin    func(provider, state, code string, chatID int64) (*LoginResult, error)

	accountKeyMu sync.RWMutex
	accountKeys  map[string]string
}

// SystemStatus represents the system status
type SystemStatus struct {
	AccountsActive int
	RouterStatus   string
	AvgLatency     time.Duration
	LastUpdate     time.Time
}

// AccountQuota represents quota info for an account
type AccountQuota struct {
	AccountID    string
	Provider     string
	Email        string
	UsagePercent float64
	IsWarning    bool
	Breakdown    []QuotaBreakdown
	IsActive     bool
	ResetAt      *time.Time
	LastCallAt   *time.Time
}

// RouterConfig represents routing configuration for display and editing.
type RouterConfig struct {
	WarningThreshold  float64
	SwitchThreshold   float64
	CriticalThreshold float64
	DefaultPolicy     string
	IgnoreEstimated   bool
	FallbackChains    map[string][]string
}

// QuotaBreakdown represents quota breakdown by group or model.
type QuotaBreakdown struct {
	Name         string
	UsagePercent float64
	IsWarning    bool
	ResetAt      *time.Time
	LastCallAt   *time.Time
	IsActive     bool
}

// AccountControl represents account routing control row.
type AccountControl struct {
	AccountID     string
	Provider      string
	Email         string
	Enabled       bool
	DisabledUntil *time.Time
	IsActive      bool
}

// AccountCheckConfig represents account availability check settings.
type AccountCheckConfig struct {
	Interval time.Duration
	Timeout  time.Duration
}

// LoginURLPayload contains OAuth URL and state for Telegram-driven login.
type LoginURLPayload struct {
	Provider     string
	URL          string
	State        string
	Mode         string
	Instructions string
}

// LoginResult is returned after OAuth callback exchange.
type LoginResult struct {
	AccountID string
	Email     string
	Provider  string
}

// ActiveAlert represents an active alert
type ActiveAlert struct {
	ID       string
	Severity string
	Message  string
	Time     time.Time
}

// DailyDigest represents the daily statistics
type DailyDigest struct {
	Date          time.Time
	TotalRequests int64
	Switches      int
	Errors        int
	TopAccounts   []string
}

// NewBot creates a new Telegram bot
func NewBot(botToken string, chatID int64, enabled bool, opts *BotOptions) *Bot {
	ctx, cancel := context.WithCancel(context.Background())

	b := &Bot{
		botToken:    botToken,
		chatID:      chatID,
		enabled:     enabled,
		sessions:    make(map[int64]*UserSession),
		ctx:         ctx,
		cancel:      cancel,
		msgChan:     make(chan Message, 100),
		alertChan:   make(chan Alert, 100),
		accountKeys: make(map[string]string),
	}

	if opts != nil {
		if opts.RateLimiter != nil {
			b.rateLimiter = opts.RateLimiter
		}
		if opts.DedupLimiter != nil {
			b.dedup = opts.DedupLimiter
		}
		if opts.BotAPI != nil {
			b.api = opts.BotAPI
		}
		if opts.Settings != nil {
			b.settings = opts.Settings
		}
	}

	// Set default rate limiter if not provided
	if b.rateLimiter == nil {
		b.rateLimiter = NewRateLimiter(30) // 30 messages per minute
	}

	// Set default dedup limiter if not provided
	if b.dedup == nil {
		b.dedup = NewDedupLimiter(5 * time.Minute)
	}

	// Load token/chat from settings if not provided
	if b.settings != nil {
		if b.botToken == "" {
			if token, ok := b.settings.Get(store.SettingTelegramBotToken); ok {
				b.botToken = token
			}
		}
		if b.chatID == 0 {
			if raw, ok := b.settings.Get(store.SettingTelegramChatID); ok && raw != "" {
				chatID := b.settings.GetInt(store.SettingTelegramChatID, 0)
				if chatID != 0 {
					b.chatID = int64(chatID)
				}
			}
		}
	}

	return b
}

// SetAutoImportEnabled sets the auto-import flag for status display.
func (b *Bot) SetAutoImportEnabled(enabled bool) {
	b.autoImportEnabled = enabled
}

// SetStatusCallback sets the callback for getting system status
func (b *Bot) SetStatusCallback(cb func() (*SystemStatus, error)) {
	b.onGetStatus = cb
}

// SetQuotasCallback sets the callback for getting quotas
func (b *Bot) SetQuotasCallback(cb func() ([]AccountQuota, error)) {
	b.onGetQuotas = cb
}

// SetAlertsCallback sets the callback for getting alerts
func (b *Bot) SetAlertsCallback(cb func() ([]ActiveAlert, error)) {
	b.onGetAlerts = cb
}

// SetMuteCallback sets the callback for muting alerts
func (b *Bot) SetMuteCallback(cb func(duration time.Duration) error) {
	b.onMuteAlerts = cb
}

// SetForceSwitchCallback sets the callback for force switching
func (b *Bot) SetForceSwitchCallback(cb func(accountID string) error) {
	b.onForceSwitch = cb
}

// SetDailyDigestCallback sets the callback for daily digest
func (b *Bot) SetDailyDigestCallback(cb func() (*DailyDigest, error)) {
	b.onGetDailyDigest = cb
}

// SetThresholdsCallback sets the callback for updating thresholds
func (b *Bot) SetThresholdsCallback(cb func(warning, switchVal, critical float64) error) {
	b.onUpdateThresholds = cb
}

// SetPolicyCallback sets the callback for updating routing policy
func (b *Bot) SetPolicyCallback(cb func(policy string) error) {
	b.onUpdatePolicy = cb
}

// SetFallbackCallback sets the callback for updating fallback chains
func (b *Bot) SetFallbackCallback(cb func(chains map[string][]string) error) {
	b.onUpdateFallbackChains = cb
}

// SetIgnoreEstimatedCallback sets the callback for toggling estimated quotas.
func (b *Bot) SetIgnoreEstimatedCallback(cb func(ignore bool) error) {
	b.onUpdateIgnoreEstimated = cb
}

// SetRouterConfigCallback sets the callback for retrieving router configuration.
func (b *Bot) SetRouterConfigCallback(cb func() (*RouterConfig, error)) {
	b.onGetRouterConfig = cb
}

// SetReloadCallback sets the callback for reloading configuration
func (b *Bot) SetReloadCallback(cb func() error) {
	b.onReloadConfig = cb
}

// SetExportCallback sets the callback for exporting configuration
func (b *Bot) SetExportCallback(cb func() (string, error)) {
	b.onExportConfig = cb
}

// SetImportCallback sets the callback for importing accounts
func (b *Bot) SetImportCallback(cb func(path string) (int, int, error)) {
	b.onImportAccounts = cb
}

// SetAccountsCallback sets callback for listing routable accounts.
func (b *Bot) SetAccountsCallback(cb func() ([]AccountControl, error)) {
	b.onGetAccounts = cb
}

// SetToggleAccountCallback sets callback for toggling account availability.
func (b *Bot) SetToggleAccountCallback(cb func(accountID string, duration time.Duration, enable bool) error) {
	b.onToggleAccount = cb
}

// SetAccountCheckConfigCallbacks configures callbacks for account availability settings.
func (b *Bot) SetAccountCheckConfigCallbacks(
	get func() (*AccountCheckConfig, error),
	set func(interval, timeout time.Duration) error,
) {
	b.onGetAccountCheckConfig = get
	b.onSetAccountCheckConfig = set
}

// SetLoginCallbacks configures Telegram-driven login flow callbacks.
func (b *Bot) SetLoginCallbacks(
	buildURL func(provider string, chatID int64) (*LoginURLPayload, error),
	complete func(provider, state, code string, chatID int64) (*LoginResult, error),
) {
	b.onBuildLoginURL = buildURL
	b.onCompleteOAuthLogin = complete
}

// Start starts the bot
func (b *Bot) Start() error {
	if !b.enabled {
		return nil
	}

	if b.botToken == "" {
		return fmt.Errorf("bot token is required")
	}

	// Start message processing loop
	b.wg.Add(1)
	go b.processMessages()

	// Start alert processing loop
	b.wg.Add(1)
	go b.processAlerts()

	// Start daily digest scheduler
	b.wg.Add(1)
	go b.scheduleDailyDigest()

	// Start dedup cleanup
	b.wg.Add(1)
	go b.dedupCleanup()

	// Start polling updates if API is configured
	if b.api != nil {
		b.wg.Add(1)
		go b.pollUpdates()
	}

	return nil
}

// Stop gracefully stops the bot
func (b *Bot) Stop() error {
	b.cancel()

	// Wait for all goroutines to finish
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for bot to stop")
	}
}

// processMessages processes incoming messages
func (b *Bot) processMessages() {
	defer b.wg.Done()

	for {
		select {
		case <-b.ctx.Done():
			return
		case msg, ok := <-b.msgChan:
			if !ok {
				return
			}
			b.handleMessage(msg)
		}
	}
}

// pollUpdates polls the Telegram API for updates and forwards them to the message channel.
func (b *Bot) pollUpdates() {
	defer b.wg.Done()

	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		updates, err := b.api.GetUpdates()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if len(updates) == 0 {
			time.Sleep(250 * time.Millisecond)
			continue
		}

		for _, msg := range updates {
			select {
			case <-b.ctx.Done():
				return
			case b.msgChan <- msg:
			default:
				// Drop if buffer is full to avoid blocking
			}
		}
	}
}

// processAlerts processes outgoing alerts
func (b *Bot) processAlerts() {
	defer b.wg.Done()

	for {
		select {
		case <-b.ctx.Done():
			return
		case alert, ok := <-b.alertChan:
			if !ok {
				return
			}
			b.handleAlert(alert)
		}
	}
}

// scheduleDailyDigest schedules and sends daily digest
func (b *Bot) scheduleDailyDigest() {
	defer b.wg.Done()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	// Schedule for a specific time (e.g., 9:00 AM)
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.sendDailyDigest()
		}
	}
}

// dedupCleanup periodically cleans up old dedup entries
func (b *Bot) dedupCleanup() {
	defer b.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.dedup.Cleanup()
		}
	}
}

// SendMessage sends a message to the configured chat
func (b *Bot) SendMessage(text string) error {
	if !b.enabled {
		return nil
	}

	if !b.rateLimiter.Allow() {
		return fmt.Errorf("rate limit exceeded")
	}

	if b.api != nil {
		return b.api.SendMessage(b.chatID, text)
	}

	return nil
}

func (b *Bot) sendMessageWithParseMode(chatID int64, text, parseMode string) {
	if !b.enabled {
		return
	}
	if !b.rateLimiter.Allow() {
		return
	}
	if b.api == nil {
		return
	}
	if sender, ok := b.api.(ParseModeSender); ok {
		_ = sender.SendMessageWithParseMode(chatID, text, parseMode)
		return
	}
	_ = b.api.SendMessage(chatID, text)
}

func (b *Bot) sendMessageWithKeyboard(chatID int64, text, parseMode string, keyboard InlineKeyboard) {
	if !b.enabled {
		return
	}
	if !b.rateLimiter.Allow() {
		return
	}
	if b.api == nil {
		return
	}
	if sender, ok := b.api.(InlineKeyboardSender); ok {
		_ = sender.SendMessageWithInlineKeyboard(chatID, text, parseMode, keyboard)
		return
	}
	if sender, ok := b.api.(ParseModeSender); ok {
		_ = sender.SendMessageWithParseMode(chatID, text, parseMode)
		return
	}
	_ = b.api.SendMessage(chatID, text)
}

// SendAlert sends an alert with deduplication
func (b *Bot) SendAlert(alert Alert) error {
	if !b.enabled {
		return nil
	}

	// Check deduplication
	key := fmt.Sprintf("alert:%s:%s", alert.ID, alert.Severity)
	if !b.dedup.CanSend(key) {
		return nil
	}

	select {
	case b.alertChan <- alert:
		return nil
	default:
		return fmt.Errorf("alert channel is full")
	}
}

// GetSession gets or creates a user session
func (b *Bot) GetSession(userID int64) *UserSession {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	if session, ok := b.sessions[userID]; ok {
		session.UpdatedAt = time.Now()
		return session
	}

	session := &UserSession{
		UserID:    userID,
		State:     StateIdle,
		Data:      make(map[string]interface{}),
		UpdatedAt: time.Now(),
	}
	b.sessions[userID] = session
	return session
}

// SetSessionState sets the state for a user session
func (b *Bot) SetSessionState(userID int64, state State, data map[string]interface{}) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	session, ok := b.sessions[userID]
	if !ok {
		session = &UserSession{
			UserID:    userID,
			State:     StateIdle,
			Data:      make(map[string]interface{}),
			UpdatedAt: time.Now(),
		}
		b.sessions[userID] = session
	}
	session.State = state
	if data != nil {
		session.Data = data
	}
	session.UpdatedAt = time.Now()
}

// ClearSession clears a user session
func (b *Bot) ClearSession(userID int64) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	delete(b.sessions, userID)
}

// CleanupSessions removes old sessions
func (b *Bot) CleanupSessions(maxAge time.Duration) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	now := time.Now()
	for userID, session := range b.sessions {
		if now.Sub(session.UpdatedAt) > maxAge {
			delete(b.sessions, userID)
		}
	}
}

// IsEnabled returns whether the bot is enabled
func (b *Bot) IsEnabled() bool {
	return b.enabled
}

// GetChatID returns the configured chat ID
func (b *Bot) GetChatID() int64 {
	return b.chatID
}
