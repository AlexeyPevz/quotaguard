package models

import (
	"fmt"
	"time"
)

// HealthStatus represents the health status of an account.
type HealthStatus struct {
	AccountID          string        `json:"account_id"`
	LastCheckedAt      time.Time     `json:"last_checked_at"`
	BaselineLatency    time.Duration `json:"baseline_latency"`
	CurrentLatency     time.Duration `json:"current_latency"`
	ErrorRate          float64       `json:"error_rate"`
	ShadowBanRisk      float64       `json:"shadow_ban_risk"`
	IsShadowBanned     bool          `json:"is_shadow_banned"`
	ConsecutiveErrors  int           `json:"consecutive_errors"`
	SuccessfulRequests int64         `json:"successful_requests"`
	FailedRequests     int64         `json:"failed_requests"`
	TotalRequests      int64         `json:"total_requests"`
}

// Validate checks if the health status is valid.
func (h *HealthStatus) Validate() error {
	if h.AccountID == "" {
		return fmt.Errorf("account ID is required")
	}
	if h.ErrorRate < 0 || h.ErrorRate > 1 {
		return fmt.Errorf("error rate must be between 0 and 1")
	}
	if h.ShadowBanRisk < 0 || h.ShadowBanRisk > 1 {
		return fmt.Errorf("shadow ban risk must be between 0 and 1")
	}
	if h.BaselineLatency < 0 {
		return fmt.Errorf("baseline latency cannot be negative")
	}
	if h.CurrentLatency < 0 {
		return fmt.Errorf("current latency cannot be negative")
	}
	return nil
}

// IsHealthy returns true if the account is healthy.
func (h *HealthStatus) IsHealthy(errorThreshold float64) bool {
	return h.ErrorRate < errorThreshold && !h.IsShadowBanned
}

// LatencySpike returns true if current latency is significantly higher than baseline.
func (h *HealthStatus) LatencySpike(multiplier float64) bool {
	if h.BaselineLatency == 0 {
		return false
	}
	return float64(h.CurrentLatency) > float64(h.BaselineLatency)*multiplier
}

// UpdateErrorRate recalculates the error rate.
func (h *HealthStatus) UpdateErrorRate() {
	total := h.SuccessfulRequests + h.FailedRequests
	if total == 0 {
		h.ErrorRate = 0
		return
	}
	h.ErrorRate = float64(h.FailedRequests) / float64(total)
}

// RecordSuccess records a successful request.
func (h *HealthStatus) RecordSuccess(latency time.Duration) {
	h.SuccessfulRequests++
	h.TotalRequests++
	h.CurrentLatency = latency
	h.ConsecutiveErrors = 0
	h.UpdateErrorRate()
	h.LastCheckedAt = time.Now()
}

// RecordFailure records a failed request.
func (h *HealthStatus) RecordFailure() {
	h.FailedRequests++
	h.TotalRequests++
	h.ConsecutiveErrors++
	h.UpdateErrorRate()
	h.LastCheckedAt = time.Now()
}

// UpdateShadowBanRisk updates the shadow ban risk based on recent behavior.
func (h *HealthStatus) UpdateShadowBanRisk() {
	// Simple heuristic: combine error rate and consecutive errors
	h.ShadowBanRisk = h.ErrorRate * 0.5
	if h.ConsecutiveErrors > 5 {
		h.ShadowBanRisk += 0.3
	}
	if h.ConsecutiveErrors > 10 {
		h.ShadowBanRisk += 0.2
	}
	if h.ShadowBanRisk > 1 {
		h.ShadowBanRisk = 1
	}
	h.IsShadowBanned = h.ShadowBanRisk > 0.8
}

// HealthStatusSlice is a slice of health statuses with helper methods.
type HealthStatusSlice []HealthStatus

// FindByAccountID returns health status by account ID.
func (hs HealthStatusSlice) FindByAccountID(id string) (*HealthStatus, bool) {
	for i := range hs {
		if hs[i].AccountID == id {
			return &hs[i], true
		}
	}
	return nil, false
}

// FilterHealthy returns only healthy accounts.
func (hs HealthStatusSlice) FilterHealthy(errorThreshold float64) HealthStatusSlice {
	var result HealthStatusSlice
	for _, h := range hs {
		if h.IsHealthy(errorThreshold) {
			result = append(result, h)
		}
	}
	return result
}

// FilterUnhealthy returns only unhealthy accounts.
func (hs HealthStatusSlice) FilterUnhealthy(errorThreshold float64) HealthStatusSlice {
	var result HealthStatusSlice
	for _, h := range hs {
		if !h.IsHealthy(errorThreshold) {
			result = append(result, h)
		}
	}
	return result
}
