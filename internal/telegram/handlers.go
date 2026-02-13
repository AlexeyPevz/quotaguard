package telegram

import (
	"fmt"
	"html"
	"net/url"
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

	if session := b.GetSession(msg.ChatID); session != nil && session.State == StateWaitingOAuth {
		b.handleLoginInput(msg.ChatID, text, session)
		return
	}

	// Inline callbacks should always be handled as commands.
	if strings.HasPrefix(text, "menu:") || strings.HasPrefix(text, "action:") {
		b.SetSessionState(msg.ChatID, StateIdle, nil)
		b.handleCommand(msg.ChatID, text)
		return
	}

	// Check rate limit
	if !b.rateLimiter.Allow() {
		b.sendErrorMessage(msg.ChatID, "Rate limit exceeded. Please try again later.")
		return
	}

	// Button-first UX: always guide to menu.
	b.SetSessionState(msg.ChatID, StateIdle, nil)
	b.handleMenu(msg.ChatID)
}

// handleCommand handles commands in idle state
func (b *Bot) handleCommand(chatID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	if strings.HasPrefix(command, "menu:") {
		b.handleMenuAction(chatID, command)
		return
	}
	if strings.HasPrefix(command, "action:") {
		b.handleMenuAction(chatID, command)
		return
	}

	switch command {
	case "/start":
		b.handleStart(chatID)
	case "/help", "/menu":
		b.handleMenu(chatID)
	default:
		// Button-only UX: always guide to menu.
		b.handleMenu(chatID)
	}
}

// handleStart handles the /start command
func (b *Bot) handleStart(chatID int64) {
	msg := "🤖 <b>QuotaGuard</b>\n\n" +
		"Я помогу мониторить квоты и управлять роутингом.\n\n" +
		"Выбери действие в меню ниже."
	b.sendMessageWithKeyboard(chatID, msg, "HTML", mainMenuKeyboard())
}

// handleHelp handles the /help command
func (b *Bot) handleHelp(chatID int64) {
	msg := formatHelpMessage()
	b.sendMessageWithKeyboard(chatID, msg, "HTML", sectionKeyboard(menuHelp))
}

// handleMenu renders the main menu
func (b *Bot) handleMenu(chatID int64) {
	msg := "📌 <b>Главное меню</b>\n\n" +
		"Статус, квоты, роутинг и настройки — всё тут."
	b.sendMessageWithKeyboard(chatID, msg, "HTML", mainMenuKeyboard())
}

func (b *Bot) handleMenuAction(chatID int64, data string) {
	switch {
	case data == menuRoot:
		b.handleMenu(chatID)
	case data == menuStatus:
		b.handleStatus(chatID)
	case data == menuQuota:
		b.handleQuota(chatID)
	case data == menuQuick:
		b.handleQuickActions(chatID)
	case data == menuAlerts:
		b.handleAlerts(chatID)
	case data == menuHelp:
		b.handleHelp(chatID)
	case data == menuRouting:
		b.handleRoutingMenu(chatID)
	case data == menuFallback:
		b.handleFallbackMenu(chatID)
	case data == menuSettings:
		b.handleSettingsMenu(chatID)
	case data == menuAccounts:
		b.handleAccountsMenu(chatID)
	case data == menuChecks:
		b.handleAccountChecksMenu(chatID)
	case data == menuConnect:
		b.handleConnectAccountsMenu(chatID)
	case strings.HasPrefix(data, actionThresholds):
		b.handleThresholdPreset(chatID, data)
	case strings.HasPrefix(data, actionPolicy):
		b.handlePolicyPreset(chatID, data)
	case strings.HasPrefix(data, actionIgnoreEst):
		b.handleIgnoreEstimated(chatID, data)
	case strings.HasPrefix(data, actionAcctDisable):
		b.handleAccountDisable(chatID, data)
	case strings.HasPrefix(data, actionAcctEnable):
		b.handleAccountEnable(chatID, data)
	case strings.HasPrefix(data, actionCheckInt):
		b.handleAccountCheckInterval(chatID, data)
	case strings.HasPrefix(data, actionCheckTO):
		b.handleAccountCheckTimeout(chatID, data)
	case strings.HasPrefix(data, actionLogin):
		b.handleAccountLogin(chatID, data)
	case data == actionReload:
		b.handleReload(chatID)
	case data == actionImport:
		b.handleImport(chatID)
	default:
		b.handleMenu(chatID)
	}
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
	b.sendMessageWithKeyboard(chatID, msg, "HTML", sectionKeyboard(menuStatus))
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
		b.sendMessageWithKeyboard(chatID, "<b>📊 Quota Status</b>\n\nNo accounts configured.", "HTML", sectionKeyboard(menuQuota))
		return
	}

	msg := formatQuotas(quotas)
	b.sendMessageWithKeyboard(chatID, msg, "HTML", sectionKeyboard(menuQuota))
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
	b.sendMessageWithKeyboard(chatID, msg, "HTML", sectionKeyboard(menuAlerts))
}

func (b *Bot) handleRoutingMenu(chatID int64) {
	if b.onGetRouterConfig == nil {
		b.sendErrorMessage(chatID, "Routing config is not available")
		return
	}
	cfg, err := b.onGetRouterConfig()
	if err != nil || cfg == nil {
		b.sendErrorMessage(chatID, "Failed to load routing config")
		return
	}

	msg := fmt.Sprintf(
		"🧭 <b>Роутинг</b>\n\n"+
			"Пороги (использование, %%):\n"+
			"• Warning: %.0f%%\n"+
			"• Switch: %.0f%%\n"+
			"• Critical: %.0f%%\n\n"+
			"Policy: <code>%s</code>\n"+
			"Estimated: <b>%s</b>\n\n"+
			"Логика:\n"+
			"• переключаем заранее на более безопасный аккаунт\n"+
			"• если у всех критично — выкачиваем до конца\n\n"+
			"Рекомендация: <b>Balanced (85/90/95)</b>.",
		cfg.WarningThreshold,
		cfg.SwitchThreshold,
		cfg.CriticalThreshold,
		html.EscapeString(cfg.DefaultPolicy),
		boolLabel(cfg.IgnoreEstimated),
	)

	b.sendMessageWithKeyboard(chatID, msg, "HTML", routingMenuKeyboard())
}

func (b *Bot) handleFallbackMenu(chatID int64) {
	if b.onGetRouterConfig == nil {
		b.sendErrorMessage(chatID, "Fallback config is not available")
		return
	}
	cfg, err := b.onGetRouterConfig()
	if err != nil || cfg == nil {
		b.sendErrorMessage(chatID, "Failed to load fallback config")
		return
	}

	msg := "🔁 <b>Фоллбэки</b>\n\n"
	if len(cfg.FallbackChains) == 0 {
		msg += "Фоллбэки не настроены.\n\n"
	} else {
		msg += formatFallbackChains(cfg.FallbackChains)
		msg += "\n"
	}
	msg += "Изменение: через конфиг и кнопки роутинга."

	b.sendMessageWithKeyboard(chatID, msg, "HTML", fallbackMenuKeyboard())
}

func (b *Bot) handleSettingsMenu(chatID int64) {
	ignoreEstimated := true
	if b.onGetRouterConfig != nil {
		if cfg, err := b.onGetRouterConfig(); err == nil && cfg != nil {
			ignoreEstimated = cfg.IgnoreEstimated
		}
	}
	msg := "⚙️ <b>Настройки</b>\n\n" +
		"Estimated аккаунты отображаются, но могут быть выключены для роутинга."
	b.sendMessageWithKeyboard(chatID, msg, "HTML", settingsMenuKeyboard(ignoreEstimated))
}

func (b *Bot) handleAccountsMenu(chatID int64) {
	if b.onGetAccounts == nil {
		b.sendErrorMessage(chatID, "Accounts callback not configured")
		return
	}
	accounts, err := b.onGetAccounts()
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to load accounts: %v", err))
		return
	}
	if len(accounts) == 0 {
		b.sendMessageWithKeyboard(chatID, "👤 <b>Аккаунты роутинга</b>\n\nНет аккаунтов для управления.", "HTML", sectionKeyboard(menuSettings))
		return
	}

	keys := make([]string, 0, len(accounts))
	var sb strings.Builder
	sb.WriteString("👤 <b>Аккаунты роутинга</b>\n\n")
	for i := range accounts {
		key := b.rememberAccountKey(accounts[i].AccountID, i)
		keys = append(keys, key)
		label := displayControlAccountLabel(accounts[i])
		status := "🟢 active"
		if !accounts[i].Enabled {
			status = "⏸ paused"
		}
		if accounts[i].IsActive {
			status += " • 🔥 in use"
		}
		sb.WriteString(fmt.Sprintf("• %s\n", label))
		sb.WriteString(fmt.Sprintf("  %s", status))
		if accounts[i].DisabledUntil != nil && !accounts[i].Enabled {
			sb.WriteString(fmt.Sprintf(" • till %s", accounts[i].DisabledUntil.Local().Format("15:04")))
		}
		sb.WriteString("\n\n")
	}

	b.sendMessageWithKeyboard(chatID, sb.String(), "HTML", accountsMenuKeyboard(accounts, keys))
}

func (b *Bot) handleQuickActions(chatID int64) {
	msg := "⚡ <b>Быстрые действия</b>\n\n" +
		"Один тап для самых частых операций."
	b.sendMessageWithKeyboard(chatID, msg, "HTML", quickActionsKeyboard())
}

func (b *Bot) handleAccountChecksMenu(chatID int64) {
	if b.onGetAccountCheckConfig == nil {
		b.sendErrorMessage(chatID, "Account checks config not available")
		return
	}
	cfg, err := b.onGetAccountCheckConfig()
	if err != nil || cfg == nil {
		b.sendErrorMessage(chatID, "Failed to load account checks config")
		return
	}
	msg := fmt.Sprintf(
		"🩺 <b>Проверка доступности аккаунтов</b>\n\n"+
			"• Интервал: <code>%s</code>\n"+
			"• Timeout: <code>%s</code>\n\n"+
			"Если аккаунт слетел по auth, бот пришлёт критический алерт.\n"+
			"После ре-логина при восстановлении придёт info-алерт.",
		cfg.Interval.Truncate(time.Second),
		cfg.Timeout.Truncate(time.Second),
	)
	b.sendMessageWithKeyboard(chatID, msg, "HTML", accountChecksMenuKeyboard())
}

func (b *Bot) handleAccountCheckInterval(chatID int64, data string) {
	if b.onSetAccountCheckConfig == nil || b.onGetAccountCheckConfig == nil {
		b.sendErrorMessage(chatID, "Account checks config not available")
		return
	}
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 {
		b.sendErrorMessage(chatID, "Invalid interval action")
		return
	}
	interval, err := time.ParseDuration(parts[2])
	if err != nil || interval <= 0 {
		b.sendErrorMessage(chatID, "Invalid interval value")
		return
	}
	current, err := b.onGetAccountCheckConfig()
	if err != nil || current == nil {
		b.sendErrorMessage(chatID, "Failed to load current config")
		return
	}
	if err := b.onSetAccountCheckConfig(interval, current.Timeout); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to update interval: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, "✅ Интервал проверки обновлён.", "HTML")
	b.handleAccountChecksMenu(chatID)
}

func (b *Bot) handleAccountCheckTimeout(chatID int64, data string) {
	if b.onSetAccountCheckConfig == nil || b.onGetAccountCheckConfig == nil {
		b.sendErrorMessage(chatID, "Account checks config not available")
		return
	}
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 {
		b.sendErrorMessage(chatID, "Invalid timeout action")
		return
	}
	timeout, err := time.ParseDuration(parts[2])
	if err != nil || timeout <= 0 {
		b.sendErrorMessage(chatID, "Invalid timeout value")
		return
	}
	current, err := b.onGetAccountCheckConfig()
	if err != nil || current == nil {
		b.sendErrorMessage(chatID, "Failed to load current config")
		return
	}
	if err := b.onSetAccountCheckConfig(current.Interval, timeout); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to update timeout: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, "✅ Timeout проверки обновлён.", "HTML")
	b.handleAccountChecksMenu(chatID)
}

func (b *Bot) handleConnectAccountsMenu(chatID int64) {
	msg := "➕ <b>Подключение аккаунтов</b>\n\n" +
		"• Antigravity/Gemini/Claude/Qwen: OAuth URL + авто-callback (через public relay, если настроен)\n" +
		"• Codex: device auth (ссылка + код)\n\n" +
		"После успеха аккаунт сразу попадёт в QuotaGuard."
	b.sendMessageWithKeyboard(chatID, msg, "HTML", connectAccountsMenuKeyboard())
}

func (b *Bot) handleAccountLogin(chatID int64, data string) {
	if b.onBuildLoginURL == nil {
		b.sendErrorMessage(chatID, "Login flow is not configured")
		return
	}
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 {
		b.sendErrorMessage(chatID, "Invalid login action")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(parts[2]))
	payload, err := b.onBuildLoginURL(provider, chatID)
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to start login: %v", err))
		return
	}
	if payload == nil {
		b.sendErrorMessage(chatID, "Login payload is empty")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(payload.Mode))
	if mode == "" {
		mode = "oauth"
	}

	if mode == "oauth" {
		if payload.URL == "" || payload.State == "" {
			b.sendErrorMessage(chatID, "Login URL is empty")
			return
		}
		b.SetSessionState(chatID, StateWaitingOAuth, map[string]interface{}{
			"provider":   provider,
			"state":      payload.State,
			"login_mode": mode,
		})

		msg := fmt.Sprintf(
			"🔐 <b>Логин: %s</b>\n\n%s\n\nПосле логина пришли сюда callback URL.",
			html.EscapeString(provider),
			html.EscapeString(payload.Instructions),
		)
		keyboard := InlineKeyboard{
			Rows: [][]InlineButton{
				{
					{Text: "🌐 Открыть OAuth", URL: payload.URL},
				},
				{
					{Text: "⬅️ Назад", CallbackData: menuConnect},
				},
			},
		}
		b.sendMessageWithKeyboard(chatID, msg, "HTML", keyboard)
		return
	}

	if mode == "token" {
		b.SetSessionState(chatID, StateWaitingOAuth, map[string]interface{}{
			"provider":   provider,
			"state":      payload.State,
			"login_mode": mode,
		})
		msg := fmt.Sprintf(
			"🔐 <b>Логин: %s</b>\n\n%s\n\nОтправь сюда session token одним сообщением.",
			html.EscapeString(provider),
			html.EscapeString(payload.Instructions),
		)
		keyboard := InlineKeyboard{
			Rows: [][]InlineButton{
				{
					{Text: "⬅️ Назад", CallbackData: menuConnect},
				},
			},
		}
		b.sendMessageWithKeyboard(chatID, msg, "HTML", keyboard)
		return
	}

	if mode == "device" {
		msg := fmt.Sprintf(
			"🔐 <b>Логин: %s</b>\n\n%s\n\nПосле завершения в браузере бот подключит аккаунт автоматически.",
			html.EscapeString(provider),
			html.EscapeString(payload.Instructions),
		)
		keyboard := InlineKeyboard{
			Rows: [][]InlineButton{
				{
					{Text: "🌐 Открыть авторизацию", URL: payload.URL},
				},
				{
					{Text: "⬅️ Назад", CallbackData: menuConnect},
				},
			},
		}
		b.sendMessageWithKeyboard(chatID, msg, "HTML", keyboard)
		return
	}

	b.sendErrorMessage(chatID, fmt.Sprintf("Unsupported login mode: %s", mode))
}

func (b *Bot) handleLoginInput(chatID int64, text string, session *UserSession) {
	mode, _ := session.Data["login_mode"].(string)
	if strings.EqualFold(mode, "token") {
		b.handleTokenLoginInput(chatID, text, session)
		return
	}
	if strings.EqualFold(mode, "device") {
		b.handleDeviceLoginInput(chatID, text, session)
		return
	}
	b.handleOAuthCallbackInput(chatID, text, session)
}

func (b *Bot) handleDeviceLoginInput(chatID int64, text string, session *UserSession) {
	if b.onCompleteOAuthLogin == nil {
		b.sendErrorMessage(chatID, "Login completion callback not configured")
		b.SetSessionState(chatID, StateIdle, nil)
		return
	}
	provider, _ := session.Data["provider"].(string)
	state, _ := session.Data["state"].(string)
	action := strings.TrimSpace(text)
	if action == "" {
		b.sendErrorMessage(chatID, "После авторизации отправь `done`")
		return
	}
	result, err := b.onCompleteOAuthLogin(provider, state, action, chatID)
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Login failed: %v", err))
		return
	}
	b.SetSessionState(chatID, StateIdle, nil)
	if result == nil {
		b.sendMessageWithParseMode(chatID, "✅ Логин завершён.", "HTML")
		b.handleConnectAccountsMenu(chatID)
		return
	}
	msg := fmt.Sprintf(
		"✅ <b>Аккаунт подключён</b>\n\n• Provider: <code>%s</code>\n• Email: <code>%s</code>\n• Account: <code>%s</code>",
		html.EscapeString(result.Provider),
		html.EscapeString(result.Email),
		html.EscapeString(result.AccountID),
	)
	b.sendMessageWithKeyboard(chatID, msg, "HTML", connectAccountsMenuKeyboard())
}

func (b *Bot) handleTokenLoginInput(chatID int64, text string, session *UserSession) {
	if b.onCompleteOAuthLogin == nil {
		b.sendErrorMessage(chatID, "Login completion callback not configured")
		b.SetSessionState(chatID, StateIdle, nil)
		return
	}
	provider, _ := session.Data["provider"].(string)
	token := strings.TrimSpace(text)
	if token == "" {
		b.sendErrorMessage(chatID, "Session token пустой")
		return
	}
	result, err := b.onCompleteOAuthLogin(provider, "manual", token, chatID)
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Login failed: %v", err))
		return
	}
	b.SetSessionState(chatID, StateIdle, nil)
	if result == nil {
		b.sendMessageWithParseMode(chatID, "✅ Логин завершён.", "HTML")
		b.handleConnectAccountsMenu(chatID)
		return
	}
	msg := fmt.Sprintf(
		"✅ <b>Аккаунт подключён</b>\n\n• Provider: <code>%s</code>\n• Email: <code>%s</code>\n• Account: <code>%s</code>",
		html.EscapeString(result.Provider),
		html.EscapeString(result.Email),
		html.EscapeString(result.AccountID),
	)
	b.sendMessageWithKeyboard(chatID, msg, "HTML", connectAccountsMenuKeyboard())
}

func (b *Bot) handleOAuthCallbackInput(chatID int64, text string, session *UserSession) {
	if b.onCompleteOAuthLogin == nil {
		b.sendErrorMessage(chatID, "Login completion callback not configured")
		b.SetSessionState(chatID, StateIdle, nil)
		return
	}
	provider, _ := session.Data["provider"].(string)
	expectedState, _ := session.Data["state"].(string)
	callbackURL := strings.TrimSpace(text)
	parsed, err := url.Parse(callbackURL)
	if err != nil || parsed == nil {
		b.sendErrorMessage(chatID, "Это невалидный callback URL")
		return
	}
	code := parsed.Query().Get("code")
	state := parsed.Query().Get("state")
	if code == "" || state == "" {
		b.sendErrorMessage(chatID, "В URL нет code/state, пришлите полный callback URL")
		return
	}
	if expectedState != "" && state != expectedState {
		b.sendErrorMessage(chatID, "State не совпадает, начни логин заново")
		return
	}

	result, err := b.onCompleteOAuthLogin(provider, state, code, chatID)
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Login failed: %v", err))
		return
	}

	b.SetSessionState(chatID, StateIdle, nil)
	if result == nil {
		b.sendMessageWithParseMode(chatID, "✅ Логин завершён.", "HTML")
		b.handleConnectAccountsMenu(chatID)
		return
	}
	msg := fmt.Sprintf(
		"✅ <b>Аккаунт подключён</b>\n\n• Provider: <code>%s</code>\n• Email: <code>%s</code>\n• Account: <code>%s</code>",
		html.EscapeString(result.Provider),
		html.EscapeString(result.Email),
		html.EscapeString(result.AccountID),
	)
	b.sendMessageWithKeyboard(chatID, msg, "HTML", connectAccountsMenuKeyboard())
}

func (b *Bot) handleThresholdPreset(chatID int64, data string) {
	if b.onUpdateThresholds == nil {
		b.sendErrorMessage(chatID, "Thresholds callback not configured")
		return
	}
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 {
		b.sendErrorMessage(chatID, "Invalid thresholds action")
		return
	}
	values := strings.Split(parts[2], ",")
	if len(values) != 3 {
		b.sendErrorMessage(chatID, "Invalid thresholds action")
		return
	}
	warn, err1 := strconv.ParseFloat(values[0], 64)
	switchVal, err2 := strconv.ParseFloat(values[1], 64)
	crit, err3 := strconv.ParseFloat(values[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		b.sendErrorMessage(chatID, "Invalid thresholds values")
		return
	}
	if err := b.onUpdateThresholds(warn, switchVal, crit); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to update thresholds: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, "✅ Пороги обновлены.", "HTML")
	b.handleRoutingMenu(chatID)
}

func (b *Bot) handlePolicyPreset(chatID int64, data string) {
	if b.onUpdatePolicy == nil {
		b.sendErrorMessage(chatID, "Policy callback not configured")
		return
	}
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 || parts[2] == "" {
		b.sendErrorMessage(chatID, "Invalid policy action")
		return
	}
	policy := parts[2]
	if err := b.onUpdatePolicy(policy); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to update policy: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, fmt.Sprintf("✅ Policy set to <code>%s</code>.", html.EscapeString(policy)), "HTML")
	b.handleRoutingMenu(chatID)
}

func (b *Bot) handleIgnoreEstimated(chatID int64, data string) {
	if b.onUpdateIgnoreEstimated == nil {
		b.sendErrorMessage(chatID, "Settings callback not configured")
		return
	}
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 {
		b.sendErrorMessage(chatID, "Invalid settings action")
		return
	}
	value := strings.ToLower(strings.TrimSpace(parts[2]))
	ignore := value == "on" || value == "true" || value == "1"
	if err := b.onUpdateIgnoreEstimated(ignore); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to update setting: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, "✅ Настройка обновлена.", "HTML")
	b.handleSettingsMenu(chatID)
}

func (b *Bot) handleReload(chatID int64) {
	if b.onReloadConfig == nil {
		b.sendErrorMessage(chatID, "Reload callback not configured")
		return
	}
	if err := b.onReloadConfig(); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to reload config: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, "✅ Конфиг перезагружен.", "HTML")
	b.handleSettingsMenu(chatID)
}

func (b *Bot) handleImport(chatID int64) {
	if b.onImportAccounts == nil {
		b.sendErrorMessage(chatID, "Import callback not configured")
		return
	}

	newCount, updatedCount, err := b.onImportAccounts("")
	if err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Import failed: %v", err))
		return
	}

	b.sendMessageWithParseMode(chatID, fmt.Sprintf("✅ Импорт завершён: %d new, %d updated", newCount, updatedCount), "HTML")
	b.handleQuickActions(chatID)
}

func (b *Bot) handleAccountDisable(chatID int64, data string) {
	if b.onToggleAccount == nil {
		b.sendErrorMessage(chatID, "Account toggle callback not configured")
		return
	}
	parts := strings.Split(data, ":")
	if len(parts) < 4 {
		b.sendErrorMessage(chatID, "Invalid disable action")
		return
	}
	key := parts[2]
	durationToken := parts[3]
	accountID, ok := b.resolveAccountKey(key)
	if !ok {
		b.sendErrorMessage(chatID, "Account key expired, refresh menu")
		return
	}
	duration, err := parseDuration(durationToken)
	if err != nil {
		b.sendErrorMessage(chatID, "Invalid disable duration")
		return
	}
	if err := b.onToggleAccount(accountID, duration, false); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to disable account: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, fmt.Sprintf("⏸ <code>%s</code> paused for %s.", html.EscapeString(accountID), duration.Truncate(time.Minute)), "HTML")
	b.handleAccountsMenu(chatID)
}

func (b *Bot) handleAccountEnable(chatID int64, data string) {
	if b.onToggleAccount == nil {
		b.sendErrorMessage(chatID, "Account toggle callback not configured")
		return
	}
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		b.sendErrorMessage(chatID, "Invalid enable action")
		return
	}
	key := parts[2]
	accountID, ok := b.resolveAccountKey(key)
	if !ok {
		b.sendErrorMessage(chatID, "Account key expired, refresh menu")
		return
	}
	if err := b.onToggleAccount(accountID, 0, true); err != nil {
		b.sendErrorMessage(chatID, fmt.Sprintf("Failed to enable account: %v", err))
		return
	}
	b.sendMessageWithParseMode(chatID, fmt.Sprintf("▶️ <code>%s</code> enabled.", html.EscapeString(accountID)), "HTML")
	b.handleAccountsMenu(chatID)
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
	b.sendMessageWithParseMode(b.chatID, msg, "HTML")
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

func (b *Bot) rememberAccountKey(accountID string, idx int) string {
	key := strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.Itoa(idx+1)
	b.accountKeyMu.Lock()
	defer b.accountKeyMu.Unlock()
	b.accountKeys[key] = accountID
	return key
}

func (b *Bot) resolveAccountKey(key string) (string, bool) {
	b.accountKeyMu.RLock()
	defer b.accountKeyMu.RUnlock()
	accountID, ok := b.accountKeys[key]
	return accountID, ok
}

func displayControlAccountLabel(acc AccountControl) string {
	if acc.Email == "" {
		return fmt.Sprintf("<code>%s</code>", html.EscapeString(acc.AccountID))
	}
	return fmt.Sprintf("<code>%s</code> %s", html.EscapeString(accountTypeFromID(acc.AccountID)), html.EscapeString(maskEmail(acc.Email)))
}
