package health

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// ControlPrompt represents a control prompt for quality checks.
type ControlPrompt struct {
	Name            string `json:"name"`
	Prompt          string `json:"prompt"`
	ExpectedPattern string `json:"expected_pattern"`
	MaxTokens       int    `json:"max_tokens"`
}

// ControlResult represents the result of a control prompt check.
type ControlResult struct {
	PromptName   string        `json:"prompt_name"`
	Passed       bool          `json:"passed"`
	ResponseTime time.Duration `json:"response_time"`
	ResponseLen  int           `json:"response_len"`
	Details      string        `json:"details"`
}

// ShadowBanRisk represents the risk level of shadow ban.
type ShadowBanRisk int

const (
	// ShadowBanRiskLow indicates low risk of shadow ban.
	ShadowBanRiskLow ShadowBanRisk = iota
	// ShadowBanRiskMedium indicates medium risk of shadow ban.
	ShadowBanRiskMedium
	// ShadowBanRiskHigh indicates high risk of shadow ban.
	ShadowBanRiskHigh
	// ShadowBanRiskCritical indicates critical risk of shadow ban.
	ShadowBanRiskCritical
)

// String returns a string representation of the shadow ban risk.
func (r ShadowBanRisk) String() string {
	switch r {
	case ShadowBanRiskLow:
		return "low"
	case ShadowBanRiskMedium:
		return "medium"
	case ShadowBanRiskHigh:
		return "high"
	case ShadowBanRiskCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// ShadowBanDetector handles shadow ban detection for accounts.
type ShadowBanDetector struct {
	ConsecutiveErrorThreshold   int
	ErrorRateThreshold          float64
	LatencyDegradationThreshold float64
	QualityCheckEnabled         bool
	ControlPrompts              []ControlPrompt
}

// NewShadowBanDetector creates a new shadow ban detector.
func NewShadowBanDetector() *ShadowBanDetector {
	return &ShadowBanDetector{
		ConsecutiveErrorThreshold:   10,
		ErrorRateThreshold:          0.15, // 15% error rate
		LatencyDegradationThreshold: 3.0,  // 3x latency increase
		QualityCheckEnabled:         false,
		ControlPrompts:              []ControlPrompt{},
	}
}

// CheckShadowBanRisk assesses the shadow ban risk for an account.
func (d *ShadowBanDetector) CheckShadowBanRisk(
	baseline *Baseline,
	consecutiveErrors int,
	totalRequests int64,
	failedRequests int64,
	currentLatency time.Duration,
) ShadowBanRisk {
	risk := ShadowBanRiskLow

	// Factor 1: Consecutive errors
	if consecutiveErrors > d.ConsecutiveErrorThreshold {
		risk = ShadowBanRiskCritical
	} else if consecutiveErrors > d.ConsecutiveErrorThreshold/2 {
		risk = ShadowBanRiskHigh
	} else if consecutiveErrors > d.ConsecutiveErrorThreshold/4 {
		risk = ShadowBanRiskMedium
	}

	// Factor 2: Error rate
	errorRate := float64(failedRequests) / float64(totalRequests)
	if totalRequests > 0 && errorRate > d.ErrorRateThreshold {
		if risk < ShadowBanRiskHigh {
			risk = ShadowBanRiskHigh
		}
	} else if totalRequests > 0 && errorRate > d.ErrorRateThreshold/2 {
		if risk < ShadowBanRiskMedium {
			risk = ShadowBanRiskMedium
		}
	}

	// Factor 3: Latency degradation
	if baseline != nil && baseline.AvgLatency > 0 {
		latencyMultiplier := float64(currentLatency) / float64(baseline.AvgLatency)
		if latencyMultiplier > d.LatencyDegradationThreshold*2 {
			if risk < ShadowBanRiskMedium {
				risk = ShadowBanRiskMedium
			}
		} else if latencyMultiplier > d.LatencyDegradationThreshold {
			if risk < ShadowBanRiskLow {
				risk = ShadowBanRiskLow
			}
		}
	}

	// Factor 4: Sample count (low sample count increases uncertainty)
	if baseline != nil && baseline.SampleCount < 10 {
		if risk > ShadowBanRiskLow {
			// Downgrade risk if we don't have enough data
			risk--
		}
	}

	return risk
}

// IsShadowBanned determines if an account is likely shadow banned.
func (d *ShadowBanDetector) IsShadowBanned(risk ShadowBanRisk) bool {
	return risk >= ShadowBanRiskHigh
}

// RunControlPrompt runs a control prompt to check response quality.
func (d *ShadowBanDetector) RunControlPrompt(ctx context.Context, prompt ControlPrompt, response string) ControlResult {
	start := time.Now()

	result := ControlResult{
		PromptName: prompt.Name,
		Passed:     true,
		Details:    "Quality check passed",
	}

	// Check response length
	if prompt.MaxTokens > 0 && len(response) > prompt.MaxTokens*4 {
		// Rough estimate: 4 characters per token
		result.Passed = false
		result.Details = "Response exceeds maximum length"
	}

	// Check expected pattern
	if prompt.ExpectedPattern != "" {
		pattern := regexp.MustCompile(prompt.ExpectedPattern)
		if !pattern.MatchString(response) {
			result.Passed = false
			result.Details = "Response does not match expected pattern"
		}
	}

	// Check for empty or very short response
	if len(strings.TrimSpace(response)) < 10 {
		result.Passed = false
		result.Details = "Response is too short or empty"
	}

	result.ResponseTime = time.Since(start)
	result.ResponseLen = len(response)

	return result
}

// AnalyzeResponse performs quality analysis on a response.
func (d *ShadowBanDetector) AnalyzeResponse(response string, expectedResponse string) ControlResult {
	start := time.Now()

	result := ControlResult{
		PromptName: "general_analysis",
		Passed:     true,
		Details:    "Response analysis passed",
	}

	// Check if response is empty or whitespace
	if len(strings.TrimSpace(response)) == 0 {
		result.Passed = false
		result.Details = "Response is empty"
		result.ResponseTime = time.Since(start)
		result.ResponseLen = 0
		return result
	}

	// Check response length
	result.ResponseLen = len(response)

	// Check for suspiciously short responses
	if len(response) < 10 {
		result.Passed = false
		result.Details = "Response is suspiciously short"
	}

	// Check if response is identical to expected (might indicate templating)
	if expectedResponse != "" && strings.TrimSpace(response) == strings.TrimSpace(expectedResponse) {
		result.Passed = false
		result.Details = "Response appears templated or identical to expected"
	}

	result.ResponseTime = time.Since(start)

	return result
}

// SetControlPrompts sets the control prompts for quality checks.
func (d *ShadowBanDetector) SetControlPrompts(prompts []ControlPrompt) {
	d.ControlPrompts = prompts
	d.QualityCheckEnabled = len(prompts) > 0
}

// EnableQualityChecks enables or disables quality checks.
func (d *ShadowBanDetector) EnableQualityChecks(enabled bool) {
	d.QualityCheckEnabled = enabled
}

// GetRiskLevel returns the risk level as a float between 0 and 1.
func (r ShadowBanRisk) GetRiskLevel() float64 {
	switch r {
	case ShadowBanRiskLow:
		return 0.25
	case ShadowBanRiskMedium:
		return 0.5
	case ShadowBanRiskHigh:
		return 0.75
	case ShadowBanRiskCritical:
		return 1.0
	default:
		return 0.0
	}
}
