package telegram

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strings"
	"time"
)

// formatStatus formats the system status message
func formatStatus(status *SystemStatus) string {
	healthEmoji := "🟢"
	if status.RouterStatus != "healthy" {
		healthEmoji = "🔴"
	}

	return fmt.Sprintf(
		"%s <b>QuotaGuard Status</b>\n\n"+
			"📊 <b>Accounts:</b> %d active\n"+
			"🔄 <b>Router:</b> %s\n"+
			"📈 <b>Avg latency:</b> %s\n\n"+
			"🕒 <b>Last update:</b> %s",
		healthEmoji,
		status.AccountsActive,
		html.EscapeString(status.RouterStatus),
		formatDuration(status.AvgLatency),
		status.LastUpdate.Format("2006-01-02 15:04:05"),
	)
}

// formatQuotas formats the quota status message with progress bars
func formatQuotas(quotas []AccountQuota) string {
	var sb strings.Builder
	sb.WriteString("<b>📊 Quota Status</b>\n\n")

	grouped := make(map[string][]AccountQuota)
	for _, q := range quotas {
		provider := strings.TrimSpace(q.Provider)
		if provider == "" {
			provider = "unknown"
		}
		grouped[provider] = append(grouped[provider], q)
	}

	providers := make([]string, 0, len(grouped))
	for provider := range grouped {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		pi := providerOrderIndex(providers[i])
		pj := providerOrderIndex(providers[j])
		if pi != pj {
			return pi < pj
		}
		return providers[i] < providers[j]
	})

	for i, provider := range providers {
		sb.WriteString(fmt.Sprintf("<b>%s</b>\n", html.EscapeString(providerTitle(provider))))

		entries := grouped[provider]
		sort.Slice(entries, func(i, j int) bool {
			return displayAccountKey(entries[i]) < displayAccountKey(entries[j])
		})

		for _, q := range entries {
			usagePct := clampPercent(q.UsagePercent)
			progressBar := renderProgressBar(usagePct, 8)
			toneEmoji := progressToneEmoji(usagePct)
			warningEmoji := ""
			if q.IsWarning {
				warningEmoji = " ⚠️"
			}

			sb.WriteString(fmt.Sprintf(
				"• <b>%s</b>\n",
				displayAccountLabel(q),
			))
			sb.WriteString(fmt.Sprintf(
				"  ↳ %s %s %.0f%%%s\n",
				toneEmoji,
				progressBar,
				usagePct,
				warningEmoji,
			))
			meta := formatQuotaMeta(q.ResetAt, q.LastCallAt, q.IsActive)
			if meta != "" {
				sb.WriteString(fmt.Sprintf("    %s\n", html.EscapeString(meta)))
			}

			for _, detail := range q.Breakdown {
				detailPct := clampPercent(detail.UsagePercent)
				detailBar := renderProgressBar(detailPct, 8)
				detailTone := progressToneEmoji(detailPct)
				detailWarning := ""
				if detail.IsWarning {
					detailWarning = " ⚠️"
				}
				sb.WriteString(fmt.Sprintf(
					"   • %s\n",
					html.EscapeString(detail.Name),
				))
				sb.WriteString(fmt.Sprintf(
					"     ↳ %s %s %.0f%%%s\n",
					detailTone,
					detailBar,
					detailPct,
					detailWarning,
				))
				detailMeta := formatQuotaMeta(detail.ResetAt, detail.LastCallAt, detail.IsActive)
				if detailMeta != "" {
					sb.WriteString(fmt.Sprintf("       %s\n", html.EscapeString(detailMeta)))
				}
			}
			sb.WriteString("\n")
		}
		if i < len(providers)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// renderProgressBar creates a text-based progress bar
func renderProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 10
	}
	filled := int(math.Floor(percent / 100.0 * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	empty := width - filled
	filledBlock := progressFillBlock(percent)
	emptyBlock := "⬛"

	var sb strings.Builder
	for i := 0; i < filled; i++ {
		sb.WriteString(filledBlock)
	}
	for i := 0; i < empty; i++ {
		sb.WriteString(emptyBlock)
	}
	return sb.String()
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func progressToneEmoji(percent float64) string {
	switch progressTone(percent) {
	case "low":
		return "🔴"
	case "mid":
		return "🟡"
	default:
		return "🟢"
	}
}

func progressTone(percent float64) string {
	switch {
	case percent < 25:
		return "low"
	case percent < 75:
		return "mid"
	default:
		return "high"
	}
}

func progressFillBlock(percent float64) string {
	switch progressTone(percent) {
	case "low":
		return "🟥"
	case "mid":
		return "🟨"
	default:
		return "🟩"
	}
}

func displayAccountKey(q AccountQuota) string {
	if q.Email != "" {
		return strings.ToLower(q.Email)
	}
	return strings.ToLower(q.AccountID)
}

func displayAccountLabel(q AccountQuota) string {
	accountType := accountTypeFromID(q.AccountID)
	if q.Email != "" {
		masked := maskEmail(q.Email)
		spoiler := fmt.Sprintf("<tg-spoiler>%s</tg-spoiler>", html.EscapeString(q.Email))
		if accountType != "" {
			return fmt.Sprintf(
				"<code>%s</code> %s %s",
				html.EscapeString(accountType),
				html.EscapeString(masked),
				spoiler,
			)
		}
		return fmt.Sprintf(
			"%s %s",
			html.EscapeString(masked),
			spoiler,
		)
	}
	if accountType != "" {
		return fmt.Sprintf("<code>%s</code>", html.EscapeString(accountType))
	}
	return fmt.Sprintf("<code>%s</code>", html.EscapeString(q.AccountID))
}

func providerOrderIndex(provider string) int {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "antigravity", "cloudcode":
		return 0
	case "codex":
		return 1
	case "gemini":
		return 2
	case "openai":
		return 3
	case "anthropic":
		return 4
	default:
		return 99
	}
}

func providerTitle(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "antigravity", "cloudcode":
		return "🛰 antigravity"
	case "codex":
		return "🧠 codex"
	case "gemini":
		return "✨ gemini"
	case "openai":
		return "⚡ openai"
	case "anthropic":
		return "🧩 anthropic"
	default:
		return provider
	}
}

func accountTypeFromID(accountID string) string {
	if accountID == "" {
		return ""
	}
	parts := strings.SplitN(accountID, "_", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	user := maskSegment(parts[0], 1)
	domain := parts[1]
	domainParts := strings.Split(domain, ".")
	if len(domainParts) == 0 {
		return user + "@***"
	}
	first := maskSegment(domainParts[0], 1)
	tld := strings.Join(domainParts[1:], ".")
	if tld != "" {
		return fmt.Sprintf("%s@%s.%s", user, first, tld)
	}
	return fmt.Sprintf("%s@%s", user, first)
}

func maskSegment(value string, keep int) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	if keep <= 0 || len(runes) <= keep {
		return string(runes[0]) + "***"
	}
	return string(runes[:keep]) + "***"
}

func boolLabel(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func formatQuotaMeta(resetAt, lastCallAt *time.Time, isActive bool) string {
	parts := make([]string, 0, 3)
	if resetAt != nil {
		until := time.Until(*resetAt)
		if until > 0 {
			parts = append(parts, "reset "+formatCompactDuration(until))
		}
	}
	if lastCallAt != nil && !lastCallAt.IsZero() {
		parts = append(parts, "last "+lastCallAt.Local().Format("15:04:05"))
	}
	if isActive {
		parts = append(parts, "active")
	}
	return strings.Join(parts, " · ")
}

func formatCompactDuration(d time.Duration) string {
	if d <= 0 {
		return "soon"
	}
	totalMin := int(d.Round(time.Minute).Minutes())
	if totalMin <= 0 {
		return "<1m"
	}
	if totalMin < 60 {
		return fmt.Sprintf("%dm", totalMin)
	}
	h := totalMin / 60
	m := totalMin % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func formatFallbackChains(chains map[string][]string) string {
	if len(chains) == 0 {
		return ""
	}
	keys := make([]string, 0, len(chains))
	for k := range chains {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, key := range keys {
		chain := chains[key]
		if len(chain) == 0 {
			continue
		}
		sb.WriteString("• ")
		sb.WriteString(html.EscapeString(key))
		sb.WriteString(": ")
		for i, item := range chain {
			if i > 0 {
				sb.WriteString(" → ")
			}
			sb.WriteString(html.EscapeString(item))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatAlerts formats the active alerts message
func formatAlerts(alerts []ActiveAlert) string {
	if len(alerts) == 0 {
		return "🛡️ <b>Active Alerts</b>\n\nНет активных алертов. Всё стабильно."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⚠️ <b>Active Alerts</b> (%d)\n\n", len(alerts)))

	for _, alert := range alerts {
		severityEmoji := getSeverityEmoji(alert.Severity)
		sb.WriteString(fmt.Sprintf(
			"%s <b>%s</b>\n"+
				"   %s\n"+
				"   <i>%s</i>\n\n",
			severityEmoji,
			strings.ToUpper(alert.Severity),
			html.EscapeString(alert.Message),
			formatTimeAgo(alert.Time),
		))
	}

	return sb.String()
}

// getSeverityEmoji returns an emoji based on severity level
func getSeverityEmoji(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "error":
		return "🔴"
	case "warning", "warn":
		return "🟡"
	case "info", "information":
		return "🔵"
	default:
		return "⚪"
	}
}

// formatTimeAgo formats a time as a relative string
func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(duration.Minutes()))
	case duration < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(duration.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(duration.Hours()/24))
	}
}

// formatMuteConfirmation formats the mute confirmation message
func formatMuteConfirmation(duration time.Duration) string {
	return fmt.Sprintf(
		"🔇 <b>Alerts Muted</b>\n\n"+
			"Алерты отключены на %s.\n\n"+
			"В это время уведомления приходить не будут.",
		formatDuration(duration),
	)
}

// formatAlert formats a single alert message
func formatAlert(alert Alert) string {
	severityEmoji := getSeverityEmoji(alert.Severity)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"%s <b>%s Alert</b>\n\n",
		severityEmoji,
		strings.ToUpper(alert.Severity),
	))

	if alert.AccountID != "" {
		sb.WriteString(fmt.Sprintf("Account: <code>%s</code>\n", html.EscapeString(alert.AccountID)))
	}

	sb.WriteString(html.EscapeString(alert.Message))
	sb.WriteString(fmt.Sprintf("\n\n<i>%s</i>", alert.Timestamp.Format("2006-01-02 15:04:05")))

	return sb.String()
}

// formatDailyDigest formats the daily digest message
func formatDailyDigest(digest *DailyDigest) string {
	var sb strings.Builder

	sb.WriteString("📈 <b>Daily Digest</b>\n")
	sb.WriteString(fmt.Sprintf("<i>%s</i>\n\n", digest.Date.Format("2006-01-02")))

	sb.WriteString("<b>Statistics:</b>\n")
	sb.WriteString(fmt.Sprintf("• Total requests: %d\n", digest.TotalRequests))
	sb.WriteString(fmt.Sprintf("• Account switches: %d\n", digest.Switches))
	sb.WriteString(fmt.Sprintf("• Errors: %d\n", digest.Errors))

	if len(digest.TopAccounts) > 0 {
		sb.WriteString("\n<b>Top Accounts:</b>\n")
		for i, acc := range digest.TopAccounts {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("• %s\n", html.EscapeString(acc)))
		}
	}

	return sb.String()
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	}

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	if hours > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dd", days)
}

// formatHelpMessage returns the help message
func formatHelpMessage() string {
	return `ℹ️ <b>Как пользоваться</b>

Используй кнопки меню — командами вводить ничего не нужно.

<b>Что можно:</b>
• Смотреть квоты и статус
• Менять политику роутинга
• Настраивать пороги переключения
• Проверять фоллбэки и алёрты

Если что-то пошло не так — просто открой меню ещё раз.`
}
