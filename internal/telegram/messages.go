package telegram

import (
	"fmt"
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
		"%s *QuotaGuard Status*\n\n"+
			"📊 Accounts: %d active\n"+
			"🔄 Router: %s\n"+
			"📈 Avg latency: %s\n\n"+
			"Last update: %s",
		healthEmoji,
		status.AccountsActive,
		status.RouterStatus,
		formatDuration(status.AvgLatency),
		status.LastUpdate.Format("2006-01-02 15:04:05"),
	)
}

// formatQuotas formats the quota status message with progress bars
func formatQuotas(quotas []AccountQuota) string {
	var sb strings.Builder
	sb.WriteString("📊 *Quota Status*\n\n")

	for _, q := range quotas {
		progressBar := renderProgressBar(q.UsagePercent, 10)
		warningEmoji := ""
		if q.IsWarning {
			warningEmoji = " ⚠️"
		}

		sb.WriteString(fmt.Sprintf(
			"`%s`: %s %.0f%% %s%s\n",
			q.AccountID,
			progressBar,
			q.UsagePercent,
			q.Provider,
			warningEmoji,
		))
	}

	return sb.String()
}

// renderProgressBar creates a text-based progress bar
func renderProgressBar(percent float64, width int) string {
	filled := int(percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	empty := width - filled

	var sb strings.Builder
	sb.WriteString("█")
	for i := 0; i < filled; i++ {
		sb.WriteString("█")
	}
	for i := 0; i < empty; i++ {
		sb.WriteString("░")
	}

	return sb.String()
}

// formatAlerts formats the active alerts message
func formatAlerts(alerts []ActiveAlert) string {
	if len(alerts) == 0 {
		return "🛡️ *Active Alerts*\n\nNo active alerts. All systems operational."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("⚠️ *Active Alerts* (%d)\n\n", len(alerts)))

	for _, alert := range alerts {
		severityEmoji := getSeverityEmoji(alert.Severity)
		sb.WriteString(fmt.Sprintf(
			"%s *%s*\n"+
				"   %s\n"+
				"   _%s_\n\n",
			severityEmoji,
			strings.ToUpper(alert.Severity),
			alert.Message,
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
		"🔇 *Alerts Muted*\n\n"+
			"Alerts are now muted for %s.\n\n"+
			"You will not receive alert notifications during this time.",
		formatDuration(duration),
	)
}

// formatAlert formats a single alert message
func formatAlert(alert Alert) string {
	severityEmoji := getSeverityEmoji(alert.Severity)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"%s *%s Alert*\n\n",
		severityEmoji,
		strings.ToUpper(alert.Severity),
	))

	if alert.AccountID != "" {
		sb.WriteString(fmt.Sprintf("Account: `%s`\n", alert.AccountID))
	}

	sb.WriteString(alert.Message)
	sb.WriteString(fmt.Sprintf("\n\n_%s_", alert.Timestamp.Format("2006-01-02 15:04:05")))

	return sb.String()
}

// formatDailyDigest formats the daily digest message
func formatDailyDigest(digest *DailyDigest) string {
	var sb strings.Builder

	sb.WriteString("📈 *Daily Digest*\n")
	sb.WriteString(fmt.Sprintf("_%s_\n\n", digest.Date.Format("2006-01-02")))

	sb.WriteString("*Statistics:*\n")
	sb.WriteString(fmt.Sprintf("• Total requests: %d\n", digest.TotalRequests))
	sb.WriteString(fmt.Sprintf("• Account switches: %d\n", digest.Switches))
	sb.WriteString(fmt.Sprintf("• Errors: %d\n", digest.Errors))

	if len(digest.TopAccounts) > 0 {
		sb.WriteString("\n*Top Accounts:*\n")
		for i, acc := range digest.TopAccounts {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("• %s\n", acc))
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
	return `📖 *QuotaGuard Bot Help*

*System Status Commands:*
/status - Show overall system status
/quota - Show quota usage for all accounts
/alerts - Show active alerts

*Alert Management:*
/mute [duration] - Mute alerts (e.g., /mute 30m, /mute 2h)

*Control Commands:*
/force_switch <account> - Force switch to specific account

*General:*
/help - Show this help message

*Duration formats:*
• 30m - 30 minutes
• 2h - 2 hours
• 1d - 1 day`
}
