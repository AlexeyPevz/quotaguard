package health

import (
	"time"
)

// AnomalyType represents the type of detected anomaly.
type AnomalyType int

const (
	// AnomalyTypeLatencySpike represents a latency spike anomaly.
	AnomalyTypeLatencySpike AnomalyType = iota
	// AnomalyTypeErrorRate represents an error rate anomaly.
	AnomalyTypeErrorRate
	// AnomalyTypeP95 represents a P95 latency anomaly.
	AnomalyTypeP95
	// AnomalyTypeTimeout represents a timeout anomaly.
	AnomalyTypeTimeout
)

// Anomaly represents a detected anomaly.
type Anomaly struct {
	AccountID   string      `json:"account_id"`
	Type        AnomalyType `json:"type"`
	ActualValue float64     `json:"actual_value"`
	Threshold   float64     `json:"threshold"`
	Timestamp   time.Time   `json:"timestamp"`
	Details     string      `json:"details"`
}

// String returns a string representation of the anomaly type.
func (t AnomalyType) String() string {
	switch t {
	case AnomalyTypeLatencySpike:
		return "latency_spike"
	case AnomalyTypeErrorRate:
		return "error_rate"
	case AnomalyTypeP95:
		return "p95_anomaly"
	case AnomalyTypeTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// AnomalyDetector handles anomaly detection for accounts.
type AnomalyDetector struct {
	LatencySpikeMultiplier float64
	P95Multiplier          float64
	ErrorRateThreshold     float64
	MinSampleCount         int
}

// NewAnomalyDetector creates a new anomaly detector with the given multipliers.
func NewAnomalyDetector(latencySpikeMultiplier, p95Multiplier float64) *AnomalyDetector {
	return &AnomalyDetector{
		LatencySpikeMultiplier: latencySpikeMultiplier,
		P95Multiplier:          p95Multiplier,
		ErrorRateThreshold:     0.1, // 10% error rate threshold
		MinSampleCount:         10,
	}
}

// CheckLatencySpike checks if the latency is a spike compared to baseline.
func (d *AnomalyDetector) CheckLatencySpike(baseline *Baseline, currentLatency time.Duration) *Anomaly {
	if baseline == nil {
		return nil
	}

	if baseline.SampleCount < d.MinSampleCount {
		return nil
	}

	if baseline.AvgLatency == 0 {
		return nil
	}

	multiplier := float64(currentLatency) / float64(baseline.AvgLatency)
	threshold := d.LatencySpikeMultiplier

	if multiplier > threshold {
		return &Anomaly{
			AccountID:   baseline.AccountID,
			Type:        AnomalyTypeLatencySpike,
			ActualValue: multiplier,
			Threshold:   threshold,
			Timestamp:   time.Now(),
			Details:     "Current latency is significantly higher than baseline average",
		}
	}

	return nil
}

// CheckErrorRate checks if the error rate exceeds the threshold.
func (d *AnomalyDetector) CheckErrorRate(baseline *Baseline, errorRate float64) *Anomaly {
	if baseline == nil {
		return nil
	}

	if errorRate > d.ErrorRateThreshold {
		return &Anomaly{
			AccountID:   baseline.AccountID,
			Type:        AnomalyTypeErrorRate,
			ActualValue: errorRate,
			Threshold:   d.ErrorRateThreshold,
			Timestamp:   time.Now(),
			Details:     "Error rate exceeds acceptable threshold",
		}
	}

	return nil
}

// CheckP95 checks if the P95 latency is anomalous.
func (d *AnomalyDetector) CheckP95(baseline *Baseline, currentLatency time.Duration) *Anomaly {
	if baseline == nil {
		return nil
	}

	if baseline.SampleCount < d.MinSampleCount {
		return nil
	}

	if baseline.P95Latency == 0 {
		return nil
	}

	multiplier := float64(currentLatency) / float64(baseline.P95Latency)
	threshold := d.P95Multiplier

	if multiplier > threshold {
		return &Anomaly{
			AccountID:   baseline.AccountID,
			Type:        AnomalyTypeP95,
			ActualValue: multiplier,
			Threshold:   threshold,
			Timestamp:   time.Now(),
			Details:     "Current latency exceeds P95 baseline by significant margin",
		}
	}

	return nil
}

// CheckTimeout checks if a request timed out.
func (d *AnomalyDetector) CheckTimeout(accountID string, timeout time.Duration) *Anomaly {
	return &Anomaly{
		AccountID:   accountID,
		Type:        AnomalyTypeTimeout,
		ActualValue: float64(timeout),
		Threshold:   0, // Any timeout is anomalous
		Timestamp:   time.Now(),
		Details:     "Request timed out",
	}
}

// DetectAnomaly performs a comprehensive anomaly check.
func (d *AnomalyDetector) DetectAnomaly(baseline *Baseline, currentLatency time.Duration, errorRate float64, timeout time.Duration) []*Anomaly {
	var anomalies []*Anomaly

	// Check timeout first (most severe)
	if timeout > 0 {
		anomaly := d.CheckTimeout(baseline.AccountID, timeout)
		if anomaly != nil {
			anomalies = append(anomalies, anomaly)
		}
		return anomalies // Timeout is the most severe, return early
	}

	// Check latency spike
	if anomaly := d.CheckLatencySpike(baseline, currentLatency); anomaly != nil {
		anomalies = append(anomalies, anomaly)
	}

	// Check P95
	if anomaly := d.CheckP95(baseline, currentLatency); anomaly != nil {
		anomalies = append(anomalies, anomaly)
	}

	// Check error rate
	if anomaly := d.CheckErrorRate(baseline, errorRate); anomaly != nil {
		anomalies = append(anomalies, anomaly)
	}

	return anomalies
}

// SetErrorRateThreshold sets the error rate threshold.
func (d *AnomalyDetector) SetErrorRateThreshold(threshold float64) {
	if threshold < 0 {
		threshold = 0
	}
	if threshold > 1 {
		threshold = 1
	}
	d.ErrorRateThreshold = threshold
}

// SetMinSampleCount sets the minimum sample count for anomaly detection.
func (d *AnomalyDetector) SetMinSampleCount(count int) {
	if count < 1 {
		count = 1
	}
	d.MinSampleCount = count
}
