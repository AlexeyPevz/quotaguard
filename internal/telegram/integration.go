package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/quotaguard/quotaguard/internal/store"
)

// BotIntegrator allows integrating QuotaGuard into existing bots
// without running its own getUpdates loop
type BotIntegrator struct {
	bot      *Bot
	handlers map[string]CommandHandler
}

// CommandHandler is a function type for handling commands
type CommandHandler func(chatID int64, args []string)

// NewBotIntegrator creates an integrator for existing bots
func NewBotIntegrator(bot *Bot) *BotIntegrator {
	return &BotIntegrator{
		bot:      bot,
		handlers: biRegisterQGBHandlers(bot),
	}
}

// HandleUpdate processes a single update from existing bot's getUpdates loop
// This way QuotaGuard doesn't run its own getUpdates and won't conflict with existing bot
func (bi *BotIntegrator) HandleUpdate(update tgbotapi.Update) {
	if update.Message != nil {
		bi.handleMessage(update.Message)
		return
	}
	if update.CallbackQuery != nil {
		bi.handleCallback(update.CallbackQuery)
	}
}

func (bi *BotIntegrator) handleMessage(msg *tgbotapi.Message) {
	if msg == nil {
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return
	}

	chatID := msg.Chat.ID
	bi.storeChatID(chatID)

	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	command := strings.TrimPrefix(parts[0], "/")
	if command == "" {
		return
	}

	// Strip bot username suffix (e.g., /qg_status@botname)
	if idx := strings.Index(command, "@"); idx != -1 {
		command = command[:idx]
	}

	args := parts[1:]

	switch {
	case command == "settoken":
		bi.bot.handleSetToken(chatID, args)
	case strings.HasPrefix(command, "qg_"):
		handlerKey := strings.TrimPrefix(command, "qg_")
		if handler, ok := bi.handlers[handlerKey]; ok {
			handler(chatID, args)
		}
	default:
		// Ignore non-QuotaGuard commands
	}
}

func (bi *BotIntegrator) handleCallback(cb *tgbotapi.CallbackQuery) {
	if cb == nil || cb.Message == nil {
		return
	}
	// Callback handling can be added when interactive UI is implemented.
}

func (bi *BotIntegrator) storeChatID(chatID int64) {
	if bi.bot == nil {
		return
	}
	if bi.bot.chatID != 0 && bi.bot.chatID == chatID {
		return
	}
	bi.bot.chatID = chatID

	if bi.bot.settings == nil {
		return
	}
	if err := bi.bot.settings.SetInt(store.SettingTelegramChatID, int(chatID)); err != nil {
		return
	}
}

// biRegisterQGBHandlers registers all QuotaGuard-specific handlers
func biRegisterQGBHandlers(bot *Bot) map[string]CommandHandler {
	return map[string]CommandHandler{
		"status":             bot.biHandleStatus,
		"fallback":           bot.biHandleFallback,
		"thresholds":         bot.biHandleThresholds,
		"policy":             bot.biHandlePolicy,
		"alerts":             bot.biHandleAlerts,
		"codex_token":        bot.biHandleCodexToken,
		"codex_status":       bot.biHandleCodexStatus,
		"antigravity_status": bot.biHandleAntigravityStatus,
		"import":             bot.biHandleImport,
		"export":             bot.biHandleExport,
		"reload":             bot.biHandleReload,
		"help":               bot.biHandleHelp,
	}
}

// biHandleStatus handles /qg_status command
func (b *Bot) biHandleStatus(chatID int64, args []string) {
	status := "📊 QuotaGuard Status\n\n"

	if b.onGetStatus != nil {
		s, err := b.onGetStatus()
		if err == nil {
			status += formatKeyValue("Active Accounts", itoa(s.AccountsActive))
			status += formatKeyValue("Router Status", s.RouterStatus)
			status += formatKeyValue("Avg Latency", s.AvgLatency.String())
			status += formatKeyValue("Last Update", s.LastUpdate.Format("15:04:05"))
			if b.autoImportEnabled {
				status += formatKeyValue("Auto-import", "enabled")
			}
		}
	}

	b.sendMessage(chatID, status)
}

// biHandleFallback handles /qg_fallback command
func (b *Bot) biHandleFallback(chatID int64, args []string) {
	if b.settings == nil {
		b.sendMessage(chatID, "⚠️ Settings store not configured")
		return
	}

	if len(args) == 0 {
		raw, ok := b.settings.Get(store.SettingFallbackChains)
		if !ok || raw == "" {
			b.sendMessage(chatID, "🔄 Fallback Configuration\n\nNo fallback chains set.")
			return
		}
		b.sendMessage(chatID, "🔄 Fallback Configuration\n\nCurrent fallback chains:\n"+raw)
		return
	}

	raw := strings.Join(args, " ")
	var chains map[string][]string
	if err := json.Unmarshal([]byte(raw), &chains); err != nil {
		b.sendMessage(chatID, "Usage: /qg_fallback {\"provider\":[\"acc1\",\"acc2\"]}")
		return
	}

	data, err := json.Marshal(chains)
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Failed to encode fallback chains: %v", err))
		return
	}

	if err := b.settings.Set(store.SettingFallbackChains, string(data)); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Failed to store fallback chains: %v", err))
		return
	}
	if b.onUpdateFallbackChains != nil {
		if err := b.onUpdateFallbackChains(chains); err != nil {
			b.sendMessage(chatID, fmt.Sprintf("Failed to apply fallback chains: %v", err))
			return
		}
	}

	b.sendMessage(chatID, "✅ Fallback chains updated.")
}

// biHandleCodexToken handles /qg_codex_token command
func (b *Bot) biHandleCodexToken(chatID int64, args []string) {
	if b.settings == nil {
		b.sendMessage(chatID, "⚠️ Settings store not configured")
		return
	}
	if len(args) == 0 {
		b.sendMessage(chatID, "Usage: /qg_codex_token <session_token>")
		return
	}
	token := strings.TrimSpace(args[0])
	if token == "" {
		b.sendMessage(chatID, "Codex session token cannot be empty")
		return
	}
	if err := b.settings.Set(store.SettingCodexSessionToken, token); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Failed to store Codex token: %v", err))
		return
	}
	b.sendMessage(chatID, "✅ Codex session token saved.")
}

// biHandleCodexStatus handles /qg_codex_status command
func (b *Bot) biHandleCodexStatus(chatID int64, args []string) {
	if b.settings == nil {
		b.sendMessage(chatID, "⚠️ Settings store not configured")
		return
	}
	token, ok := b.settings.Get(store.SettingCodexSessionToken)
	if !ok || strings.TrimSpace(token) == "" {
		b.sendMessage(chatID, "ℹ️ Codex: not configured. Use /qg_codex_token <session_token>.")
		return
	}
	b.sendMessage(chatID, "✅ Codex: session token configured.")
}

// biHandleAntigravityStatus handles /qg_antigravity_status command
func (b *Bot) biHandleAntigravityStatus(chatID int64, args []string) {
	port := strings.TrimSpace(os.Getenv("QUOTAGUARD_ANTIGRAVITY_PORT"))
	csrf := strings.TrimSpace(os.Getenv("QUOTAGUARD_ANTIGRAVITY_CSRF"))
	msg := "🛰 Antigravity Status\n\n"

	if port != "" {
		msg += formatKeyValue("Port", port)
	} else {
		msg += formatKeyValue("Port", "not set (auto-detect)")
	}
	if csrf != "" {
		msg += formatKeyValue("CSRF", "set")
	} else {
		msg += formatKeyValue("CSRF", "not set (auto-detect)")
	}

	startCmd := strings.TrimSpace(os.Getenv("QUOTAGUARD_ANTIGRAVITY_START_CMD"))
	if startCmd != "" {
		msg += formatKeyValue("Auto-start", "custom command configured")
	} else {
		msg += formatKeyValue("Auto-start", "PATH lookup")
	}

	b.sendMessage(chatID, msg)
}

// biHandleThresholds handles /qg_thresholds command
func (b *Bot) biHandleThresholds(chatID int64, args []string) {
	if b.settings == nil {
		b.sendMessage(chatID, "⚠️ Settings store not configured")
		return
	}

	if len(args) == 0 {
		warning := b.settings.GetFloat(store.SettingThresholdsWarning, 0)
		switchVal := b.settings.GetFloat(store.SettingThresholdsSwitch, 0)
		critical := b.settings.GetFloat(store.SettingThresholdsCritical, 0)
		msg := "📈 Threshold Configuration\n\n" +
			formatKeyValue("Warning", fmt.Sprintf("%.2f", warning)) +
			formatKeyValue("Switch", fmt.Sprintf("%.2f", switchVal)) +
			formatKeyValue("Critical", fmt.Sprintf("%.2f", critical)) +
			"\nUpdate: /qg_thresholds <warning> <switch> <critical>"
		b.sendMessage(chatID, msg)
		return
	}

	if len(args) != 3 {
		b.sendMessage(chatID, "Usage: /qg_thresholds <warning> <switch> <critical>")
		return
	}

	warning, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		b.sendMessage(chatID, "Invalid warning threshold")
		return
	}
	switchVal, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		b.sendMessage(chatID, "Invalid switch threshold")
		return
	}
	critical, err := strconv.ParseFloat(args[2], 64)
	if err != nil {
		b.sendMessage(chatID, "Invalid critical threshold")
		return
	}

	if err := b.settings.SetFloat(store.SettingThresholdsWarning, warning); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Failed to store warning threshold: %v", err))
		return
	}
	if err := b.settings.SetFloat(store.SettingThresholdsSwitch, switchVal); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Failed to store switch threshold: %v", err))
		return
	}
	if err := b.settings.SetFloat(store.SettingThresholdsCritical, critical); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Failed to store critical threshold: %v", err))
		return
	}

	if b.onUpdateThresholds != nil {
		if err := b.onUpdateThresholds(warning, switchVal, critical); err != nil {
			b.sendMessage(chatID, fmt.Sprintf("Failed to apply thresholds: %v", err))
			return
		}
	}

	b.sendMessage(chatID, "✅ Thresholds updated.")
}

// biHandlePolicy handles /qg_policy command
func (b *Bot) biHandlePolicy(chatID int64, args []string) {
	if b.settings == nil {
		b.sendMessage(chatID, "⚠️ Settings store not configured")
		return
	}

	if len(args) == 0 {
		policy, _ := b.settings.Get(store.SettingRoutingPolicy)
		if policy == "" {
			policy = "balanced"
		}
		msg := "⚖️ Routing Policy\n\nCurrent policy: " + policy + "\n\n" +
			"Update: /qg_policy <policy>"
		b.sendMessage(chatID, msg)
		return
	}

	policy := strings.TrimSpace(strings.ToLower(args[0]))
	if policy == "" {
		b.sendMessage(chatID, "Usage: /qg_policy <policy>")
		return
	}

	if err := b.settings.Set(store.SettingRoutingPolicy, policy); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Failed to store policy: %v", err))
		return
	}
	if b.onUpdatePolicy != nil {
		if err := b.onUpdatePolicy(policy); err != nil {
			b.sendMessage(chatID, fmt.Sprintf("Failed to apply policy: %v", err))
			return
		}
	}

	b.sendMessage(chatID, fmt.Sprintf("✅ Routing policy set to %s.", policy))
}

// biHandleAlerts handles /qg_alerts command
func (b *Bot) biHandleAlerts(chatID int64, args []string) {
	alertText := "🔔 Alerts Configuration\n\n"

	if b.onGetAlerts != nil {
		alerts, err := b.onGetAlerts()
		if err == nil && len(alerts) > 0 {
			for _, alert := range alerts {
				if len(alertText) > 500 {
					alertText += "...\n"
					break
				}
				alertText += formatKeyValue(alert.Severity, alert.Message)
			}
		}
	}

	alertText += "\nConfigure via config.yaml\nEdit telegram.alerts section."
	b.sendMessage(chatID, alertText)
}

// biHandleImport handles /qg_import command
func (b *Bot) biHandleImport(chatID int64, args []string) {
	if b.onImportAccounts == nil {
		b.sendMessage(chatID, "📥 Import Accounts\n\nRun: quotaguard import /path/to/auths\nThis will discover accounts from CLIProxyAPI auth files.")
		return
	}
	path := ""
	if len(args) > 0 {
		path = args[0]
	}
	newCount, updatedCount, err := b.onImportAccounts(path)
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Import failed: %v", err))
		return
	}
	b.sendMessage(chatID, fmt.Sprintf("✅ Import complete: %d new, %d updated", newCount, updatedCount))
}

// biHandleExport handles /qg_export command
func (b *Bot) biHandleExport(chatID int64, args []string) {
	if b.onExportConfig == nil {
		b.sendMessage(chatID, "📤 Export Configuration\n\nCurrent config.yaml content:\n[Send config file manually]")
		return
	}
	content, err := b.onExportConfig()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Export failed: %v", err))
		return
	}
	sendLongMessage(b, chatID, "📤 Export Configuration\n\n"+content)
}

// biHandleReload handles /qg_reload command
func (b *Bot) biHandleReload(chatID int64, args []string) {
	if b.onReloadConfig == nil {
		b.sendMessage(chatID, "🔄 Reload Configuration\n\nReload handler not configured.")
		return
	}
	if err := b.onReloadConfig(); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("Reload failed: %v", err))
		return
	}
	b.sendMessage(chatID, "🔄 Reload Configuration\n\nConfiguration reloaded successfully.")
}

// biHandleHelp handles /qg_help command
func (b *Bot) biHandleHelp(chatID int64, args []string) {
	helpText := "🤖 QuotaGuard Bot Commands\n\n" +
		"/qg_status - Show system status\n" +
		"/qg_fallback - Configure fallback chains\n" +
		"/qg_thresholds - Set thresholds\n" +
		"/qg_policy - Change routing policy\n" +
		"/qg_alerts - Show active alerts\n" +
		"/qg_codex_token - Store Codex session token\n" +
		"/qg_codex_status - Show Codex auth status\n" +
		"/qg_antigravity_status - Show Antigravity detection status\n" +
		"/qg_import - Import CLIProxyAPI accounts\n" +
		"/qg_export - Export configuration\n" +
		"/qg_reload - Reload config\n" +
		"/qg_help - Show this help\n\n" +
		"/settoken - Configure bot token"

	b.sendMessage(chatID, helpText)
}

// Helper functions for session state management
func (b *Bot) setSessionState(chatID int64, state State) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	if b.sessions[chatID] == nil {
		b.sessions[chatID] = &UserSession{
			UserID: chatID,
			Data:   make(map[string]interface{}),
		}
	}
	b.sessions[chatID].State = state
	b.sessions[chatID].UpdatedAt = time.Now()
}

func (b *Bot) getSessionState(chatID int64) State {
	b.sessionsMu.RLock()
	defer b.sessionsMu.RUnlock()

	if session, ok := b.sessions[chatID]; ok {
		return session.State
	}
	return StateIdle
}

func (b *Bot) clearSessionState(chatID int64) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	if b.sessions[chatID] != nil {
		b.sessions[chatID].State = StateIdle
	}
}

func (b *Bot) setSessionData(chatID int64, key string, value interface{}) {
	b.sessionsMu.Lock()
	defer b.sessionsMu.Unlock()

	if b.sessions[chatID] == nil {
		b.sessions[chatID] = &UserSession{
			UserID: chatID,
			Data:   make(map[string]interface{}),
		}
	}
	b.sessions[chatID].Data[key] = value
	b.sessions[chatID].UpdatedAt = time.Now()
}

func (b *Bot) getSessionData(chatID int64, key string) string {
	b.sessionsMu.RLock()
	defer b.sessionsMu.RUnlock()

	if b.sessions[chatID] != nil {
		if val, ok := b.sessions[chatID].Data[key]; ok {
			if str, ok := val.(string); ok {
				return str
			}
		}
	}
	return ""
}

// formatKeyValue formats a key-value pair
func formatKeyValue(key, value string) string {
	return "• " + key + ": " + value + "\n"
}

// itoa converts int to string
func itoa(n int) string {
	return strconv.Itoa(n)
}

const maxTelegramMessageLen = 3500

func sendLongMessage(b *Bot, chatID int64, text string) {
	if len(text) <= maxTelegramMessageLen {
		b.sendMessage(chatID, text)
		return
	}

	remaining := text
	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > maxTelegramMessageLen {
			chunk = remaining[:maxTelegramMessageLen]
		}
		b.sendMessage(chatID, chunk)
		remaining = remaining[len(chunk):]
	}
}

// Constants for state management
const (
	StateWaitingToken        State = "waiting_token"
	StateWaitingTokenConfirm State = "waiting_token_confirm"
)
