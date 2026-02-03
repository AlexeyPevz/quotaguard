package mocks

import (
	"sync"
	"time"
)

// MockTelegramBot implements a mock Telegram bot for testing
type MockTelegramBot struct {
	SentMessages []SentMessage
	SentCount    int
	Errors       []error
	mu           sync.Mutex
}

// SentMessage represents a sent message
type SentMessage struct {
	ChatID    int64
	Text      string
	ParseMode string
	Time      time.Time
}

// AlertSeverity represents alert severity levels
type AlertSeverity string

const (
	SeverityCritical AlertSeverity = "critical"
	SeverityWarning  AlertSeverity = "warning"
	SeverityInfo     AlertSeverity = "info"
)

// MockAlert represents a mock alert
type MockAlert struct {
	ID        string
	Title     string
	Message   string
	AccountID string
	Severity  AlertSeverity
	ChatID    int64
	CreatedAt time.Time
}

// NewMockTelegramBot creates a new mock Telegram bot
func NewMockTelegramBot() *MockTelegramBot {
	return &MockTelegramBot{
		SentMessages: make([]SentMessage, 0),
		Errors:       make([]error, 0),
	}
}

// SendMessage simulates sending a message
func (m *MockTelegramBot) SendMessage(chatID int64, text string, parseMode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SentCount++
	m.SentMessages = append(m.SentMessages, SentMessage{
		ChatID:    chatID,
		Text:      text,
		ParseMode: parseMode,
		Time:      time.Now(),
	})

	return nil
}

// SendAlert sends an alert message
func (m *MockTelegramBot) SendAlert(alert *MockAlert) error {
	return m.SendMessage(
		alert.ChatID,
		formatAlertMessage(alert),
		"Markdown",
	)
}

// SendHealthNotification sends a health notification
func (m *MockTelegramBot) SendHealthNotification(status string, details string) error {
	text := formatHealthNotification(status, details)
	return m.SendMessage(0, text, "Markdown")
}

// GetSentMessages returns all sent messages
func (m *MockTelegramBot) GetSentMessages() []SentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SentMessages
}

// GetSentCount returns the number of sent messages
func (m *MockTelegramBot) GetSentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SentCount
}

// ClearSentMessages clears the sent messages
func (m *MockTelegramBot) ClearSentMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentMessages = make([]SentMessage, 0)
	m.SentCount = 0
}

// AddError simulates an error
func (m *MockTelegramBot) AddError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Errors = append(m.Errors, err)
}

// formatAlertMessage formats an alert message
func formatAlertMessage(alert *MockAlert) string {
	severityEmoji := "⚠️"
	switch alert.Severity {
	case SeverityCritical:
		severityEmoji = "🚨"
	case SeverityWarning:
		severityEmoji = "⚠️"
	case SeverityInfo:
		severityEmoji = "ℹ️"
	}

	return severityEmoji + " *Alert: " + alert.Title + "*\n\n" +
		alert.Message + "\n\n" +
		"*Account:* `" + alert.AccountID + "`\n" +
		"*Severity:* " + string(alert.Severity) + "\n" +
		"*Time:* " + alert.CreatedAt.Format(time.RFC3339)
}

// formatHealthNotification formats a health notification
func formatHealthNotification(status string, details string) string {
	statusEmoji := "✅"
	if status != "healthy" {
		statusEmoji = "❌"
	}

	return statusEmoji + " *System Health Check*\n\n" +
		"*Status:* " + status + "\n\n" +
		details
}

// MockTelegramHandler implements a mock handler for telegram updates
type MockTelegramHandler struct {
	Updates   []TelegramUpdate
	Responses []string
	mu        sync.Mutex
}

// TelegramUpdate represents a mock telegram update
type TelegramUpdate struct {
	UpdateID int64
	Message  TelegramMessage
	Time     time.Time
}

// TelegramMessage represents a mock telegram message
type TelegramMessage struct {
	MessageID int64
	Chat      TelegramChat
	Text      string
	From      TelegramUser
}

// TelegramChat represents a mock telegram chat
type TelegramChat struct {
	ID   int64
	Type string
}

// TelegramUser represents a mock telegram user
type TelegramUser struct {
	ID        int64
	FirstName string
	Username  string
}

// NewMockTelegramHandler creates a new mock handler
func NewMockTelegramHandler() *MockTelegramHandler {
	return &MockTelegramHandler{
		Updates:   make([]TelegramUpdate, 0),
		Responses: make([]string, 0),
	}
}

// ProcessUpdate processes a mock update
func (m *MockTelegramHandler) ProcessUpdate(update TelegramUpdate) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Updates = append(m.Updates, update)

	// Simple command processing
	text := update.Message.Text
	var response string

	switch {
	case text == "/status":
		response = "✅ System is running normally"
	case text == "/health":
		response = "🏥 All components healthy"
	case text == "/start":
		response = "👋 Welcome to QuotaGuard Bot!"
	case text == "/help":
		response = "Available commands:\n/status - Check system status\n/health - Check component health\n/start - Start the bot\n/help - Show this help"
	default:
		response = "Unknown command: " + text
	}

	m.Responses = append(m.Responses, response)
	return response
}

// GetUpdates returns all updates
func (m *MockTelegramHandler) GetUpdates() []TelegramUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Updates
}

// GetResponses returns all responses
func (m *MockTelegramHandler) GetResponses() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Responses
}
