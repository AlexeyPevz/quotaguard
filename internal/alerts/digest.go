package alerts

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DigestScheduler manages daily digest scheduling
type DigestScheduler struct {
	timezone   *time.Location
	digestTime string // Format: "HH:MM"
	generateFn func() (*DigestData, error)
	sendFn     func(*DigestData) error

	// Control
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	running bool
}

// NewDigestScheduler creates a new digest scheduler
func NewDigestScheduler(timezone string, digestTime string, generateFn func() (*DigestData, error), sendFn func(*DigestData) error) (*DigestScheduler, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	if digestTime == "" {
		digestTime = "09:00"
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &DigestScheduler{
		timezone:   loc,
		digestTime: digestTime,
		generateFn: generateFn,
		sendFn:     sendFn,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Start starts the digest scheduler
func (d *DigestScheduler) Start() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return
	}

	d.running = true
	d.wg.Add(1)
	go d.run()
}

// Stop stops the digest scheduler
func (d *DigestScheduler) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	d.cancel()
	d.wg.Wait()
	d.running = false
}

// IsRunning returns whether the scheduler is running
func (d *DigestScheduler) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// run is the main scheduler loop
func (d *DigestScheduler) run() {
	defer d.wg.Done()

	// Calculate initial delay until next scheduled time
	delay := d.calculateNextDelay()
	timer := time.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-timer.C:
			d.sendDigest()
			// Reset timer for next day
			timer.Reset(24 * time.Hour)
		}
	}
}

// calculateNextDelay calculates the delay until the next scheduled time
func (d *DigestScheduler) calculateNextDelay() time.Duration {
	now := time.Now().In(d.timezone)

	// Parse digest time
	var hour, minute int
	if _, err := fmt.Sscanf(d.digestTime, "%d:%d", &hour, &minute); err != nil {
		hour = 0
		minute = 0
	}

	// Create target time for today
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, d.timezone)

	// If target time has passed, schedule for tomorrow
	if target.Before(now) {
		target = target.Add(24 * time.Hour)
	}

	return time.Until(target)
}

// sendDigest generates and sends the digest
func (d *DigestScheduler) sendDigest() {
	if d.generateFn == nil || d.sendFn == nil {
		return
	}

	data, err := d.generateFn()
	if err != nil {
		return
	}

	_ = d.sendFn(data)
}

// GenerateDigest generates a digest from alerts and account data
func GenerateDigest(alerts []Alert, accountUsages []AccountUsage) *DigestData {
	now := time.Now()

	// Count alerts by severity
	alertSummary := make(map[Severity]*AlertSummary)
	for _, alert := range alerts {
		if summary, exists := alertSummary[alert.Severity]; exists {
			summary.Count++
			if alert.Timestamp.After(summary.LastAt) {
				summary.LastAt = alert.Timestamp
			}
		} else {
			alertSummary[alert.Severity] = &AlertSummary{
				Severity: alert.Severity,
				Count:    1,
				LastAt:   alert.Timestamp,
			}
		}
	}

	// Convert map to slice
	summaries := make([]AlertSummary, 0, len(alertSummary))
	for _, s := range alertSummary {
		summaries = append(summaries, *s)
	}

	// Sort by severity (critical first)
	sort.Slice(summaries, func(i, j int) bool {
		severityOrder := map[Severity]int{
			SeverityCritical: 0,
			SeverityWarning:  1,
			SeverityInfo:     2,
		}
		return severityOrder[summaries[i].Severity] < severityOrder[summaries[j].Severity]
	})

	// Sort accounts by usage
	sortedAccounts := make([]AccountUsage, len(accountUsages))
	copy(sortedAccounts, accountUsages)
	sort.Slice(sortedAccounts, func(i, j int) bool {
		return sortedAccounts[i].UsagePercent > sortedAccounts[j].UsagePercent
	})

	// Take top 5
	if len(sortedAccounts) > 5 {
		sortedAccounts = sortedAccounts[:5]
	}

	return &DigestData{
		Date:        now,
		TopAccounts: sortedAccounts,
		Alerts:      summaries,
	}
}

// FormatDigest formats a digest for display
func FormatDigest(digest *DigestData) string {
	var result string

	result = fmt.Sprintf("📈 *Daily Digest* - %s\n\n", digest.Date.Format("2006-01-02"))

	// Alert summary
	if len(digest.Alerts) > 0 {
		result += "*Alert Summary:*\n"
		for _, summary := range digest.Alerts {
			emoji := "🔵"
			switch summary.Severity {
			case SeverityCritical:
				emoji = "🔴"
			case SeverityWarning:
				emoji = "🟡"
			}
			result += fmt.Sprintf("%s %s: %d\n", emoji, summary.Severity, summary.Count)
		}
		result += "\n"
	}

	// Top accounts
	if len(digest.TopAccounts) > 0 {
		result += "*Top Accounts by Usage:*\n"
		for _, acc := range digest.TopAccounts {
			result += fmt.Sprintf("• %s (%s): %.1f%%\n", acc.AccountID, acc.Provider, acc.UsagePercent)
		}
	}

	return result
}
