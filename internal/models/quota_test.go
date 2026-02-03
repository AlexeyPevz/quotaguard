package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaInfo_Validate(t *testing.T) {
	tests := []struct {
		name    string
		quota   QuotaInfo
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid quota info",
			quota: QuotaInfo{
				Provider:    ProviderOpenAI,
				AccountID:   "acc-1",
				Tier:        "tier-1",
				Confidence:  1.0,
				CollectedAt: time.Now(),
			},
			wantErr: false,
		},
		{
			name: "missing account ID",
			quota: QuotaInfo{
				Provider:    ProviderOpenAI,
				Confidence:  1.0,
				CollectedAt: time.Now(),
			},
			wantErr: true,
			errMsg:  "account ID is required",
		},
		{
			name: "missing provider",
			quota: QuotaInfo{
				AccountID:   "acc-1",
				Confidence:  1.0,
				CollectedAt: time.Now(),
			},
			wantErr: true,
			errMsg:  "provider is required",
		},
		{
			name: "invalid dimension",
			quota: QuotaInfo{
				Provider:  ProviderOpenAI,
				AccountID: "acc-1",
				Dimensions: DimensionSlice{
					{Type: "", Limit: 100, Remaining: 50, Confidence: 1.0},
				},
				Confidence:  1.0,
				CollectedAt: time.Now(),
			},
			wantErr: true,
			errMsg:  "dimension type is required",
		},
		{
			name: "confidence too high",
			quota: QuotaInfo{
				Provider:    ProviderOpenAI,
				AccountID:   "acc-1",
				Confidence:  1.5,
				CollectedAt: time.Now(),
			},
			wantErr: true,
			errMsg:  "confidence must be between 0 and 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.quota.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestQuotaInfo_UpdateEffective(t *testing.T) {
	tests := []struct {
		name                 string
		dimensions           DimensionSlice
		expectedEffectivePct float64
		expectedCriticalType DimensionType
	}{
		{
			name:                 "empty dimensions",
			dimensions:           DimensionSlice{},
			expectedEffectivePct: 0,
		},
		{
			name: "single dimension",
			dimensions: DimensionSlice{
				{Type: DimensionRPM, Limit: 100, Remaining: 25},
			},
			expectedEffectivePct: 25.0,
			expectedCriticalType: DimensionRPM,
		},
		{
			name: "multiple dimensions - TPM critical",
			dimensions: DimensionSlice{
				{Type: DimensionRPM, Limit: 100, Remaining: 50},   // 50%
				{Type: DimensionTPM, Limit: 1000, Remaining: 100}, // 10% - critical
			},
			expectedEffectivePct: 10.0,
			expectedCriticalType: DimensionTPM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := QuotaInfo{
				Dimensions: tt.dimensions,
			}
			q.UpdateEffective()

			assert.InDelta(t, tt.expectedEffectivePct, q.EffectiveRemainingPct, 0.0001)
			if tt.expectedCriticalType != "" {
				require.NotNil(t, q.CriticalDimension)
				assert.Equal(t, tt.expectedCriticalType, q.CriticalDimension.Type)
			} else {
				assert.Nil(t, q.CriticalDimension)
			}
		})
	}
}

func TestQuotaInfo_IsExhausted(t *testing.T) {
	tests := []struct {
		name     string
		dims     DimensionSlice
		expected bool
	}{
		{
			name:     "no dimensions",
			dims:     DimensionSlice{},
			expected: false,
		},
		{
			name: "none exhausted",
			dims: DimensionSlice{
				{Limit: 100, Remaining: 50},
				{Limit: 100, Remaining: 25},
			},
			expected: false,
		},
		{
			name: "one exhausted",
			dims: DimensionSlice{
				{Limit: 100, Remaining: 50},
				{Limit: 100, Remaining: 0},
			},
			expected: true,
		},
		{
			name: "all exhausted",
			dims: DimensionSlice{
				{Limit: 100, Remaining: 0},
				{Limit: 100, Remaining: 0},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := QuotaInfo{Dimensions: tt.dims}
			got := q.IsExhausted()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestQuotaInfo_IsCritical(t *testing.T) {
	q := QuotaInfo{EffectiveRemainingPct: 10.0}

	tests := []struct {
		name      string
		threshold float64
		expected  bool
	}{
		// IsCritical returns true if EffectiveRemainingPct < threshold
		// 10% < 5% = false, so not critical
		{"below threshold (5%)", 5.0, false},
		// 10% < 10% = false, at threshold is not critical
		{"at threshold (10%)", 10.0, false},
		// 10% < 15% = true, so critical
		{"above threshold (15%)", 15.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := q.IsCritical(tt.threshold)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestQuotaInfo_EffectiveRemainingWithVirtual(t *testing.T) {
	tests := []struct {
		name           string
		effectivePct   float64
		virtualUsedPct float64
		expected       float64
	}{
		{"no virtual used", 50.0, 0, 50.0},
		{"with virtual used", 50.0, 10.0, 40.0},
		{"virtual exceeds effective", 50.0, 60.0, -10.0},
		{"zero effective", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := QuotaInfo{
				EffectiveRemainingPct: tt.effectivePct,
				VirtualUsedPercent:    tt.virtualUsedPct,
			}
			got := q.EffectiveRemainingWithVirtual()
			assert.InDelta(t, tt.expected, got, 0.0001)
		})
	}
}

func TestQuotaInfo_AddVirtualUsed(t *testing.T) {
	q := QuotaInfo{VirtualUsedPercent: 10.0}

	q.AddVirtualUsed(5.0)
	assert.InDelta(t, 15.0, q.VirtualUsedPercent, 0.0001)

	// Should not go below 0
	q.AddVirtualUsed(-20.0)
	assert.InDelta(t, 0.0, q.VirtualUsedPercent, 0.0001)
}

func TestQuotaInfo_ReleaseVirtualUsed(t *testing.T) {
	q := QuotaInfo{VirtualUsedPercent: 10.0}

	q.ReleaseVirtualUsed(5.0)
	assert.InDelta(t, 5.0, q.VirtualUsedPercent, 0.0001)

	// Should not go below 0
	q.ReleaseVirtualUsed(10.0)
	assert.InDelta(t, 0.0, q.VirtualUsedPercent, 0.0001)
}

func TestQuotaInfoSlice_FindByAccountID(t *testing.T) {
	quotas := QuotaInfoSlice{
		{AccountID: "acc-1", Provider: ProviderOpenAI},
		{AccountID: "acc-2", Provider: ProviderAnthropic},
		{AccountID: "acc-3", Provider: ProviderGemini},
	}

	tests := []struct {
		name         string
		id           string
		wantFound    bool
		wantProvider Provider
	}{
		{"find acc-1", "acc-1", true, ProviderOpenAI},
		{"find acc-2", "acc-2", true, ProviderAnthropic},
		{"find unknown", "acc-999", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, found := quotas.FindByAccountID(tt.id)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantProvider, q.Provider)
			}
		})
	}
}

func TestQuotaInfoSlice_FilterByProvider(t *testing.T) {
	quotas := QuotaInfoSlice{
		{AccountID: "acc-1", Provider: ProviderOpenAI},
		{AccountID: "acc-2", Provider: ProviderAnthropic},
		{AccountID: "acc-3", Provider: ProviderOpenAI},
	}

	filtered := quotas.FilterByProvider(ProviderOpenAI)

	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-1", filtered[0].AccountID)
	assert.Equal(t, "acc-3", filtered[1].AccountID)
}

func TestQuotaInfoSlice_FilterAvailable(t *testing.T) {
	quotas := QuotaInfoSlice{
		{AccountID: "acc-1", IsThrottled: false, IsShadowBanned: false, Dimensions: DimensionSlice{{Limit: 100, Remaining: 50}}},
		{AccountID: "acc-2", IsThrottled: true, IsShadowBanned: false, Dimensions: DimensionSlice{{Limit: 100, Remaining: 50}}},
		{AccountID: "acc-3", IsThrottled: false, IsShadowBanned: true, Dimensions: DimensionSlice{{Limit: 100, Remaining: 50}}},
		{AccountID: "acc-4", IsThrottled: false, IsShadowBanned: false, Dimensions: DimensionSlice{{Limit: 100, Remaining: 0}}},
		{AccountID: "acc-5", IsThrottled: false, IsShadowBanned: false, Dimensions: DimensionSlice{{Limit: 100, Remaining: 25}}},
	}

	filtered := quotas.FilterAvailable()

	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-1", filtered[0].AccountID)
	assert.Equal(t, "acc-5", filtered[1].AccountID)
}

func TestQuotaInfoSlice_SortByEffectiveRemaining(t *testing.T) {
	quotas := QuotaInfoSlice{
		{AccountID: "acc-1", EffectiveRemainingPct: 50.0, VirtualUsedPercent: 10.0}, // 40% effective
		{AccountID: "acc-2", EffectiveRemainingPct: 80.0, VirtualUsedPercent: 0},    // 80% effective
		{AccountID: "acc-3", EffectiveRemainingPct: 30.0, VirtualUsedPercent: 5.0},  // 25% effective
	}

	sorted := quotas.SortByEffectiveRemaining()

	// Should be sorted by effective remaining (with virtual) descending
	assert.Equal(t, "acc-2", sorted[0].AccountID) // 80%
	assert.Equal(t, "acc-1", sorted[1].AccountID) // 40%
	assert.Equal(t, "acc-3", sorted[2].AccountID) // 25%
}

func TestQuotaInfo_JSON(t *testing.T) {
	collectedAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	resetAt := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)

	quota := QuotaInfo{
		Provider:  ProviderOpenAI,
		AccountID: "acc-1",
		Tier:      "tier-1",
		Dimensions: DimensionSlice{
			{Type: DimensionRPM, Limit: 100, Remaining: 50, Confidence: 1.0},
		},
		EffectiveRemainingPct: 50.0,
		CriticalDimension:     &Dimension{Type: DimensionRPM, Limit: 100, Remaining: 50},
		Source:                SourceHeaders,
		Confidence:            1.0,
		CollectedAt:           collectedAt,
		IsThrottled:           false,
		IsShadowBanned:        false,
		VirtualUsedPercent:    10.0,
	}
	quota.CriticalDimension.ResetAt = &resetAt

	// Test marshal/unmarshal
	data, err := json.Marshal(quota)
	require.NoError(t, err)

	var decoded QuotaInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, quota.Provider, decoded.Provider)
	assert.Equal(t, quota.AccountID, decoded.AccountID)
	assert.Equal(t, quota.Tier, decoded.Tier)
	assert.Equal(t, quota.EffectiveRemainingPct, decoded.EffectiveRemainingPct)
	assert.Equal(t, quota.Source, decoded.Source)
	assert.Equal(t, quota.Confidence, decoded.Confidence)
	assert.Equal(t, quota.IsThrottled, decoded.IsThrottled)
	assert.Equal(t, quota.IsShadowBanned, decoded.IsShadowBanned)
	assert.Equal(t, quota.VirtualUsedPercent, decoded.VirtualUsedPercent)
	assert.True(t, quota.CollectedAt.Equal(decoded.CollectedAt))
	require.NotNil(t, decoded.CriticalDimension)
	assert.Equal(t, quota.CriticalDimension.Type, decoded.CriticalDimension.Type)
}
