package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/quotaguard/quotaguard/internal/store"
)

// handleMessage processes an incoming message
func (b *Bot) handleMessage(msg Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Get or create user session
	session := b.GetSession(msg.ChatID)

	// Check rate limit
	if !b.rateLimiter.Allow() {
		b.sendErrorMessage(msg.ChatID, "Rate limit exceeded. Please try again later.")
		return
	}

	// Handle based on current state
	switch session.State {
	case StateIdle:
		b.handleCommand(msg.ChatID, text)
	case StateWaitingMute:
		b.handleMuteDuration(msg.ChatID, text)
	case StateWaitingSwitch:
		b.handleSwitchAccount(msg.ChatID, text)
	case StateConfirming:
		b.handleConfirmation(msg.ChatID, text, session)
	}
}

// handleCommand handles commands in idle state
func (b *Bot) handleCommand(chatID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	switch command {
	case "/start":
		b.handleStart(chatID)
	case "/help":
		b.handleHelp(chatID)
	case "/status":
		b.handleStatus(chatID)
	case "/quota":
		b.handleQuota(chatID)
	case "/alerts":
		b.handleAlerts(chatID)
	case "/mute":
		b.handleMute(chatID, args)
	case "/force_switch":
		b.handleForceSwitch(chatID, args)
	case "/settoken":
		b.handleSetToken(chatID, args)
	case "/qg_status":
		b.biHandleStatus(chatID, args)
	case "/qg_alerts":
		b.biHandleAlerts(chatID, args)
	case "/qg_thresholds":
		b.biHandleThresholds(chatID, args)
	case "/qg_policy":
		b.biHandlePolicy(chatID, args)
	case "/qg_fallback":
		b.biHandleFallback(chatID, args)
	case "/qg_codex_token":
		b.biHandleCodexToken(chatID, args)
	case "/qg_codex_status":
		b.biHandleCodexStatus(chatID, args)
	case "/qg_import":
		b.biHandleImport(chatID, args)
	case "/qg_export":
		b.biHandleExport(chatID, args)
	case "/qg_reload":
		b.biHandleReload(chatID, args)
	default:
		b.sendErrorMessage(chatID, fmt.Sprintf("Unknown command: %s. Type /help for available commands.", command))
	}
}

// handleStart handles the /start command
func (b *Bot) handleStart(chatID int64) {
	msg := `🤖 *QuotaGuard Bot*

Welcome! I'm here to help you monitor and manage your QuotaGuard system.

Type /help to see available commands.`
	b.sendMessage(chatID, msg)
}

// handleHelp handles the /help command
func (b *Bot) handleHelp(chatID int64) {
	msg := `📖 *Available Commands*

*System Status*
/status - Show overall system status
/quota - Show quota usage for all accounts
/alerts - Show active alerts

*Alert Management*
/mute [duration] - Mute alerts for specified duration (e.g., /mute 30m, /mute 2h)

*Control*
/force_switch <account> - Force switch to a specific account

*Setup*
/settoken <token> - Store bot token in settings
/qg_codex_token <session_token> - Store Codex session token
/qg_codex_status - Show Codex auth status

*General*
/help - Show this help message

*Examples:*
/quota - Show all quotas
/mute 1h - Mute alerts for 1 hour
/force_switch openai-1 - Switch to openai-1 account`
	b.sendMessage(chatID, msg)
}

// handleSetToken handles the /settoken command
func (b *Bot) handleSetToken(chatID int64, args []string) {
	if len(args) == 0 {
		b.sendMessage(chatID, "Usage: /settoken <telegram_bot_token>")
		return
	}
	if b.settings == nil {
		b.sendErrorMessage(chatID, "Settings store is not configured")
		return
	}

	token := strings.TrimSpace(args[0])
	if token == "" {
		b.sendErrorMessage(chatID, "Token cannot be empty")
		return
	}

	if err := b.settings.Set(store.SettingTelegramBotToken, token); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to store token: %v", err))
		return
	}
	if err := b.settings.SetInt(store.SettingTelegramChatID, int(chatID)); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to store chat_id: %v", err))
		return
	}

	b.botToken = token
	b.chatID = chatID
	b.sendMessage(chatID, "✅ Token saved to settings")
}

// handleStatus handles the /status command
func (b *Bot) handleStatus(chatID int64) {
	if b.onGetStatus == nil {
		b.sendErrorMessage(chatID, "Status callback not configured")
		return
	}

	status, err := b.onGetStatus()
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to get status: %v", err))
		return
	}

	msg := formatStatus(status)
	b.sendMessage(chatID, msg)
}

// handleQuota handles the /quota command
func (b *Bot) handleQuota(chatID int64) {
	if b.onGetQuotas == nil {
		b.sendErrorMessage(chatID, "Quotas callback not configured")
		return
	}

	quotas, err := b.onGetQuotas()
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to get quotas: %v", err))
		return
	}

	if len(quotas) == 0 {
		b.sendMessage(chatID, "📊 *Quota Status*\n\nNo accounts configured.")
		return
	}

	msg := formatQuotas(quotas)
	b.sendMessage(chatID, msg)
}

// handleAlerts handles the /alerts command
func (b *Bot) handleAlerts(chatID int64) {
	if b.onGetAlerts == nil {
		b.sendErrorMessage(chatID, "Alerts callback not configured")
		return
	}

	alerts, err := b.onGetAlerts()
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to get alerts: %v", err))
		return
	}

	msg := formatAlerts(alerts)
	b.sendMessage(chatID, msg)
}

// handleMute handles the /mute command
func (b *Bot) handleMute(chatID int64, args []string) {
	if b.onMuteAlerts == nil {
		b.sendErrorMessage(chatID, "Mute callback not configured")
		return
	}

	// If no duration provided, ask for it
	if len(args) == 0 {
		b.SetSessionState(chatID, StateWaitingMute, nil)
		b.sendMessage(chatID, "⏱️ *Mute Alerts*\n\nPlease specify the duration (e.g., 30m, 2h, 1d):")
		return
	}

	// Parse duration
	duration, err := parseDuration(args[0])
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Invalid duration: %v. Use format like 30m, 2h, 1d", err))
		return
	}

	// Apply mute
	if err := b.onMuteAlerts(duration); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to mute alerts: %v", err))
		return
	}

	msg := formatMuteConfirmation(duration)
	b.sendMessage(chatID, msg)
}

// handleMuteDuration handles duration input in waiting_mute state
func (b *Bot) handleMuteDuration(chatID int64, text string) {
	duration, err := parseDuration(strings.TrimSpace(text))
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Invalid duration: %v. Use format like 30m, 2h, 1d", err))
		return
	}

	if b.onMuteAlerts == nil {
		b.sendErrorMessage(chatID, "Mute callback not configured")
		b.ClearSession(chatID)
		return
	}

	if err := b.onMuteAlerts(duration); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to mute alerts: %v", err))
		b.ClearSession(chatID)
		return
	}

	b.ClearSession(chatID)
	msg := formatMuteConfirmation(duration)
	b.sendMessage(chatID, msg)
}

// handleForceSwitch handles the /force_switch command
func (b *Bot) handleForceSwitch(chatID int64, args []string) {
	if b.onForceSwitch == nil {
		b.sendErrorMessage(chatID, "Force switch callback not configured")
		return
	}

	// If no account provided, ask for it
	if len(args) == 0 {
		b.SetSessionState(chatID, StateWaitingSwitch, nil)
		b.sendMessage(chatID, "🔄 *Force Switch*\n\nPlease specify the account ID to switch to:")
		return
	}

	accountID := args[0]

	// Confirm before switching
	b.SetSessionState(chatID, StateConfirming, map[string]interface{}{
		"action":    "force_switch",
		"accountID": accountID,
	})

	msg := fmt.Sprintf("⚠️ *Confirm Force Switch*\n\nSwitch to account: `%s`\n\nReply with 'yes' to confirm or 'no' to cancel.", accountID)
	b.sendMessage(chatID, msg)
}

// handleSwitchAccount handles account input in waiting_switch state
func (b *Bot) handleSwitchAccount(chatID int64, text string) {
	accountID := strings.TrimSpace(text)
	if accountID == "" {
		b.sendErrorMessage(chatID, "Account ID cannot be empty. Please provide a valid account ID:")
		return
	}

	// Confirm before switching
	b.SetSessionState(chatID, StateConfirming, map[string]interface{}{
		"action":    "force_switch",
		"accountID": accountID,
	})

	msg := fmt.Sprintf("⚠️ *Confirm Force Switch*\n\nSwitch to account: `%s`\n\nReply with 'yes' to confirm or 'no' to cancel.", accountID)
	b.sendMessage(chatID, msg)
}

// handleConfirmation handles confirmation responses
func (b *Bot) handleConfirmation(chatID int64, text string, session *UserSession) {
	response := strings.ToLower(strings.TrimSpace(text))

	if response != "yes" && response != "y" {
		b.ClearSession(chatID)
		b.sendMessage(chatID, "❌ Operation cancelled.")
		return
	}

	action, ok := session.Data["action"].(string)
	if !ok {
		b.ClearSession(chatID)
		b.sendErrorMessage(chatID, "Invalid session data")
		return
	}

	switch action {
	case "force_switch":
		accountID, ok := session.Data["accountID"].(string)
		if !ok {
			b.ClearSession(chatID)
			b.sendErrorMessage(chatID, "Invalid account ID in session")
			return
		}

		if b.onForceSwitch == nil {
			b.ClearSession(chatID)
			b.sendErrorMessage(chatID, "Force switch callback not configured")
			return
		}

		if err := b.onForceSwitch(accountID); err != nil {
			b.ClearSession(chatID)
			b.sendErrorMessage(chatID, fmt.Sprintf("Failed to switch account: %v", err))
			return
		}

		b.ClearSession(chatID)
		msg := fmt.Sprintf("✅ *Switch Successful*\n\nSuccessfully switched to account: `%s`", accountID)
		b.sendMessage(chatID, msg)

	default:
		b.ClearSession(chatID)
		b.sendErrorMessage(chatID, "Unknown action")
	}
}

// handleAlert processes and sends an alert
func (b *Bot) handleAlert(alert Alert) {
	if !b.rateLimiter.Allow() {
		return // Silently drop if rate limited
	}

	msg := formatAlert(alert)
	b.sendMessage(b.chatID, msg)
}

// sendDailyDigest sends the daily digest message
func (b *Bot) sendDailyDigest() {
	if b.onGetDailyDigest == nil {
		return
	}

	digest, err := b.onGetDailyDigest()
	if err != nil {
		return
	}

	msg := formatDailyDigest(digest)
	b.sendMessage(b.chatID, msg)
}

// sendMessage sends a message to a chat
func (b *Bot) sendMessage(chatID int64, text string) {
	if b.api != nil {
		_ = b.api.SendMessage(chatID, text)
	}
}

// sendErrorMessage sends an error message to a chat
func (b *Bot) sendErrorMessage(chatID int64, text string) {
	msg := fmt.Sprintf("❌ *Error*\n\n%s", text)
	b.sendMessage(chatID, msg)
}

// parseDuration parses a duration string (e.g., "30m", "2h", "1d")
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Try standard duration parsing first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Parse custom format (e.g., "30m", "2h", "1d")
	s = strings.ToLower(s)

	var numStr string
	var unit string

	for i, c := range s {
		if c >= '0' && c <= '9' {
			numStr += string(c)
		} else {
			unit = s[i:]
			break
		}
	}

	if numStr == "" {
		return 0, fmt.Errorf("no number found")
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %v", err)
	}

	switch unit {
	case "m", "min", "minute", "minutes":
		return time.Duration(num) * time.Minute, nil
	case "h", "hr", "hour", "hours":
		return time.Duration(num) * time.Hour, nil
	case "d", "day", "days":
		return time.Duration(num) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}
}
