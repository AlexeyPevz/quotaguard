package models

import (
	"fmt"
	"sort"
	"time"

	"github.com/quotaguard/quotaguard/internal/errors"
)

// QuotaInfo represents quota information for an account.
type QuotaInfo struct {
	Provider              Provider       `json:"provider"`
	AccountID             string         `json:"account_id"`
	Tier                  string         `json:"tier"`
	Dimensions            DimensionSlice `json:"dimensions"`
	EffectiveRemainingPct float64        `json:"effective_remaining_percent"`
	CriticalDimension     *Dimension     `json:"critical_dimension,omitempty"`
	Source                Source         `json:"source"`
	Confidence            float64        `json:"confidence"`
	CollectedAt           time.Time      `json:"collected_at"`
	IsThrottled           bool           `json:"is_throttled"`
	IsShadowBanned        bool           `json:"is_shadow_banned"`
	VirtualUsedPercent    float64        `json:"virtual_used_percent"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

// NewQuotaInfo creates a new QuotaInfo.
func NewQuotaInfo() *QuotaInfo {
	return &QuotaInfo{
		Dimensions: make(DimensionSlice, 0),
		UpdatedAt:  time.Now(),
	}
}

// Get returns the used value for a dimension.
func (q *QuotaInfo) Get(dim DimensionType) int64 {
	for _, d := range q.Dimensions {
		if d.Type == dim {
			return d.Used
		}
	}
	return 0
}

// Set sets the used value for a dimension.
func (q *QuotaInfo) Set(dim DimensionType, value int64) {
	q.UpdatedAt = time.Now()
	for i := range q.Dimensions {
		if q.Dimensions[i].Type == dim {
			q.Dimensions[i].Used = value
			return
		}
	}
	// If not found, add it (assuming default limit for now, or just setting usage)
	// Note: Ideally we should know the limit. For now, we just update usage.
	q.Dimensions = append(q.Dimensions, Dimension{
		Type: dim,
		Used: value,
		// Limit is unknown here, should be set by collector
	})
}

// Validate checks if the quota info is valid.
func (q *QuotaInfo) Validate() error {
	if q.AccountID == "" {
		return &errors.ErrQuotaValidation{Field: "account_id", AccountID: q.AccountID, Err: fmt.Errorf("account ID is required")}
	}
	if q.Provider == "" {
		return &errors.ErrQuotaValidation{Field: "provider", AccountID: q.AccountID, Err: fmt.Errorf("provider is required")}
	}
	for i, dim := range q.Dimensions {
		if err := dim.Validate(); err != nil {
			return &errors.ErrQuotaValidation{Field: fmt.Sprintf("dimension[%d]", i), AccountID: q.AccountID, Err: err}
		}
	}
	if q.Confidence < 0 || q.Confidence > 1 {
		return &errors.ErrQuotaValidation{Field: "confidence", AccountID: q.AccountID, Err: fmt.Errorf("confidence must be between 0 and 1")}
	}
	return nil
}

// UpdateEffective calculates effective remaining percent and critical dimension.
func (q *QuotaInfo) UpdateEffective() {
	if len(q.Dimensions) == 0 {
		q.EffectiveRemainingPct = 0
		q.CriticalDimension = nil
		return
	}

	q.EffectiveRemainingPct = q.Dimensions.MinRemainingPercent()
	q.CriticalDimension = q.Dimensions.CriticalDimension()
}

// IsExhausted returns true if any dimension is exhausted.
func (q *QuotaInfo) IsExhausted() bool {
	for _, dim := range q.Dimensions {
		if dim.IsExhausted() {
			return true
		}
	}
	return false
}

// IsCritical returns true if effective remaining is below threshold.
func (q *QuotaInfo) IsCritical(threshold float64) bool {
	return q.EffectiveRemainingPct < threshold
}

// EffectiveRemainingWithVirtual returns effective remaining minus virtual used.
func (q *QuotaInfo) EffectiveRemainingWithVirtual() float64 {
	return q.EffectiveRemainingPct - q.VirtualUsedPercent
}

// AddVirtualUsed adds to virtual used percentage.
func (q *QuotaInfo) AddVirtualUsed(percent float64) {
	q.VirtualUsedPercent += percent
	if q.VirtualUsedPercent < 0 {
		q.VirtualUsedPercent = 0
	}
}

// ReleaseVirtualUsed subtracts from virtual used percentage.
func (q *QuotaInfo) ReleaseVirtualUsed(percent float64) {
	q.VirtualUsedPercent -= percent
	if q.VirtualUsedPercent < 0 {
		q.VirtualUsedPercent = 0
	}
}

// QuotaInfoSlice is a slice of quota info with helper methods.
type QuotaInfoSlice []QuotaInfo

// FindByAccountID returns quota info by account ID.
func (qs QuotaInfoSlice) FindByAccountID(id string) (*QuotaInfo, bool) {
	for i := range qs {
		if qs[i].AccountID == id {
			return &qs[i], true
		}
	}
	return nil, false
}

// FilterByProvider returns quota info for a specific provider.
func (qs QuotaInfoSlice) FilterByProvider(p Provider) QuotaInfoSlice {
	var result QuotaInfoSlice
	for _, q := range qs {
		if q.Provider == p {
			result = append(result, q)
		}
	}
	return result
}

// FilterAvailable returns quota info for non-exhausted accounts.
func (qs QuotaInfoSlice) FilterAvailable() QuotaInfoSlice {
	var result QuotaInfoSlice
	for _, q := range qs {
		if !q.IsExhausted() && !q.IsThrottled && !q.IsShadowBanned {
			result = append(result, q)
		}
	}
	return result
}

// SortByEffectiveRemaining sorts by effective remaining percent (descending).
func (qs QuotaInfoSlice) SortByEffectiveRemaining() QuotaInfoSlice {
	result := make(QuotaInfoSlice, len(qs))
	copy(result, qs)

	sort.Slice(result, func(i, j int) bool {
		return result[i].EffectiveRemainingWithVirtual() > result[j].EffectiveRemainingWithVirtual()
	})

	return result
}
