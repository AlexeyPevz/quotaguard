package models

import (
	"fmt"
	"time"
)

// DimensionType represents the type of quota dimension.
type DimensionType string

const (
	DimensionRPM          DimensionType = "RPM"
	DimensionTPM          DimensionType = "TPM"
	DimensionRPD          DimensionType = "RPD"
	DimensionTPD          DimensionType = "TPD"
	DimensionBudget       DimensionType = "BUDGET"
	DimensionSubscription DimensionType = "SUBSCRIPTION"
)

// WindowSemantics defines how the limit window operates.
type WindowSemantics string

const (
	WindowFixed   WindowSemantics = "FIXED_WINDOW"
	WindowToken   WindowSemantics = "TOKEN_BUCKET"
	WindowUnknown WindowSemantics = "UNKNOWN"
)

// Source indicates how the dimension data was obtained.
type Source string

const (
	SourceHeaders   Source = "HEADERS"
	SourcePolling   Source = "POLLING"
	SourceEstimated Source = "ESTIMATED"
	SourceCached    Source = "CACHED"
)

// Dimension represents a single quota limit dimension.
type Dimension struct {
	Name       string          `json:"name,omitempty"`
	Type       DimensionType   `json:"type"`
	Limit      int64           `json:"limit"`
	Used       int64           `json:"used"`
	Remaining  int64           `json:"remaining"`
	ResetAt    *time.Time      `json:"reset_at,omitempty"`
	RefillRate float64         `json:"refill_rate,omitempty"`
	Semantics  WindowSemantics `json:"semantics"`
	Source     Source          `json:"source"`
	Confidence float64         `json:"confidence"`
}

// Validate checks if the dimension is valid.
func (d *Dimension) Validate() error {
	if d.Type == "" {
		return fmt.Errorf("dimension type is required")
	}
	if d.Limit < 0 {
		return fmt.Errorf("limit cannot be negative")
	}
	if d.Remaining < 0 {
		return fmt.Errorf("remaining cannot be negative")
	}
	if d.Remaining > d.Limit {
		return fmt.Errorf("remaining cannot exceed limit")
	}
	if d.Confidence < 0 || d.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

// RemainingPercent returns the percentage of remaining quota.
func (d *Dimension) RemainingPercent() float64 {
	if d.Limit == 0 {
		return 0
	}
	return float64(d.Remaining) / float64(d.Limit) * 100
}

// IsExhausted returns true if the quota is exhausted.
func (d *Dimension) IsExhausted() bool {
	return d.Remaining <= 0
}

// IsCritical returns true if the remaining quota is below the threshold.
func (d *Dimension) IsCritical(threshold float64) bool {
	return d.RemainingPercent() < threshold
}

// TimeUntilReset returns the duration until reset, or 0 if no reset time.
func (d *Dimension) TimeUntilReset() time.Duration {
	if d.ResetAt == nil {
		return 0
	}
	until := time.Until(*d.ResetAt)
	if until < 0 {
		return 0
	}
	return until
}

// DimensionSlice is a slice of dimensions with helper methods.
type DimensionSlice []Dimension

// FindByType returns the first dimension of the given type.
func (ds DimensionSlice) FindByType(dt DimensionType) (*Dimension, bool) {
	for i := range ds {
		if ds[i].Type == dt {
			return &ds[i], true
		}
	}
	return nil, false
}

// MinRemainingPercent returns the minimum remaining percent across all dimensions.
func (ds DimensionSlice) MinRemainingPercent() float64 {
	if len(ds) == 0 {
		return 0
	}
	min := ds[0].RemainingPercent()
	for i := 1; i < len(ds); i++ {
		if p := ds[i].RemainingPercent(); p < min {
			min = p
		}
	}
	return min
}

// CriticalDimension returns the dimension with the minimum remaining percent.
func (ds DimensionSlice) CriticalDimension() *Dimension {
	if len(ds) == 0 {
		return nil
	}
	minIdx := 0
	minPct := ds[0].RemainingPercent()
	for i := 1; i < len(ds); i++ {
		if p := ds[i].RemainingPercent(); p < minPct {
			minPct = p
			minIdx = i
		}
	}
	return &ds[minIdx]
}
