package health

import (
	"math"
	"sort"
	"time"
)

// Baseline represents baseline metrics for an account.
type Baseline struct {
	AccountID      string          `json:"account_id"`
	AvgLatency     time.Duration   `json:"avg_latency"`
	P50Latency     time.Duration   `json:"p50_latency"`
	P95Latency     time.Duration   `json:"p95_latency"`
	P99Latency     time.Duration   `json:"p99_latency"`
	ErrorRate      float64         `json:"error_rate"`
	SampleCount    int             `json:"sample_count"`
	LastUpdatedAt  time.Time       `json:"last_updated_at"`
	LatencyHistory []time.Duration `json:"-"`
	MaxHistorySize int             `json:"-"`
}

// NewBaseline creates a new baseline for an account.
func NewBaseline(accountID string) *Baseline {
	return &Baseline{
		AccountID:      accountID,
		MaxHistorySize: 1000,
		LatencyHistory: make([]time.Duration, 0),
	}
}

// UpdateBaseline updates the baseline with new latency data.
func (b *Baseline) UpdateBaseline(latency time.Duration, success bool) {
	// Add to history
	b.LatencyHistory = append(b.LatencyHistory, latency)
	if len(b.LatencyHistory) > b.MaxHistorySize {
		b.LatencyHistory = b.LatencyHistory[len(b.LatencyHistory)-b.MaxHistorySize:]
	}

	// Recalculate metrics
	b.Recalculate()
	b.LastUpdatedAt = time.Now()
	b.SampleCount = len(b.LatencyHistory)
}

// Recalculate recalculates all baseline metrics.
func (b *Baseline) Recalculate() {
	if len(b.LatencyHistory) == 0 {
		return
	}

	// Calculate average latency
	var totalLatency int64
	for _, lat := range b.LatencyHistory {
		totalLatency += int64(lat)
	}
	b.AvgLatency = time.Duration(totalLatency / int64(len(b.LatencyHistory)))

	// Calculate percentiles
	sorted := make([]time.Duration, len(b.LatencyHistory))
	copy(sorted, b.LatencyHistory)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	b.P50Latency = percentile(sorted, 50)
	b.P95Latency = percentile(sorted, 95)
	b.P99Latency = percentile(sorted, 99)
}

// IsAnomaly checks if the given value is an anomaly based on baseline.
func (b *Baseline) IsAnomaly(latency time.Duration, spikeMultiplier, p95Multiplier float64) bool {
	if b.SampleCount < 10 {
		// Not enough data for reliable anomaly detection
		return false
	}

	// Check latency spike
	if b.AvgLatency > 0 && float64(latency) > float64(b.AvgLatency)*spikeMultiplier {
		return true
	}

	// Check P95 anomaly
	if b.P95Latency > 0 && float64(latency) > float64(b.P95Latency)*p95Multiplier {
		return true
	}

	return false
}

// CalculatePercentile calculates the p-th percentile of the baseline latencies.
func (b *Baseline) CalculatePercentile(p float64) time.Duration {
	if len(b.LatencyHistory) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(b.LatencyHistory))
	copy(sorted, b.LatencyHistory)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	return percentile(sorted, p)
}

// percentile calculates the p-th percentile of a sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	// Handle edge cases
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}

	// Calculate percentile
	index := (p / 100) * float64(len(sorted)-1)
	lower := int(index)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[lower]
	}

	// Interpolate
	weight := index - float64(lower)
	latency := float64(sorted[lower])*(1-weight) + float64(sorted[upper])*weight
	return time.Duration(math.Round(latency))
}

// GetLatencyHistory returns a copy of the latency history.
func (b *Baseline) GetLatencyHistory() []time.Duration {
	result := make([]time.Duration, len(b.LatencyHistory))
	copy(result, b.LatencyHistory)
	return result
}

// ClearHistory clears the latency history.
func (b *Baseline) ClearHistory() {
	b.LatencyHistory = make([]time.Duration, 0)
	b.SampleCount = 0
}

// GetErrorRate returns the current error rate.
func (b *Baseline) GetErrorRate() float64 {
	return b.ErrorRate
}

// SetErrorRate sets the error rate.
func (b *Baseline) SetErrorRate(rate float64) {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	b.ErrorRate = rate
}
