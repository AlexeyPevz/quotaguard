package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDimension_Validate(t *testing.T) {
	tests := []struct {
		name    string
		dim     Dimension
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid dimension",
			dim: Dimension{
				Type:       DimensionRPM,
				Limit:      100,
				Remaining:  50,
				Confidence: 1.0,
			},
			wantErr: false,
		},
		{
			name: "missing type",
			dim: Dimension{
				Limit:      100,
				Remaining:  50,
				Confidence: 1.0,
			},
			wantErr: true,
			errMsg:  "dimension type is required",
		},
		{
			name: "negative limit",
			dim: Dimension{
				Type:       DimensionRPM,
				Limit:      -1,
				Remaining:  50,
				Confidence: 1.0,
			},
			wantErr: true,
			errMsg:  "limit cannot be negative",
		},
		{
			name: "negative remaining",
			dim: Dimension{
				Type:       DimensionRPM,
				Limit:      100,
				Remaining:  -1,
				Confidence: 1.0,
			},
			wantErr: true,
			errMsg:  "remaining cannot be negative",
		},
		{
			name: "remaining exceeds limit",
			dim: Dimension{
				Type:       DimensionRPM,
				Limit:      100,
				Remaining:  150,
				Confidence: 1.0,
			},
			wantErr: true,
			errMsg:  "remaining cannot exceed limit",
		},
		{
			name: "confidence too low",
			dim: Dimension{
				Type:       DimensionRPM,
				Limit:      100,
				Remaining:  50,
				Confidence: -0.1,
			},
			wantErr: true,
			errMsg:  "confidence must be between 0 and 1",
		},
		{
			name: "confidence too high",
			dim: Dimension{
				Type:       DimensionRPM,
				Limit:      100,
				Remaining:  50,
				Confidence: 1.1,
			},
			wantErr: true,
			errMsg:  "confidence must be between 0 and 1",
		},
		{
			name: "zero limit and remaining",
			dim: Dimension{
				Type:       DimensionBudget,
				Limit:      0,
				Remaining:  0,
				Confidence: 1.0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dim.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDimension_RemainingPercent(t *testing.T) {
	tests := []struct {
		name     string
		dim      Dimension
		expected float64
	}{
		{
			name:     "50% remaining",
			dim:      Dimension{Limit: 100, Remaining: 50},
			expected: 50.0,
		},
		{
			name:     "100% remaining",
			dim:      Dimension{Limit: 100, Remaining: 100},
			expected: 100.0,
		},
		{
			name:     "0% remaining",
			dim:      Dimension{Limit: 100, Remaining: 0},
			expected: 0.0,
		},
		{
			name:     "zero limit",
			dim:      Dimension{Limit: 0, Remaining: 0},
			expected: 0.0,
		},
		{
			name:     "33.33% remaining",
			dim:      Dimension{Limit: 300, Remaining: 100},
			expected: 33.33333333333333,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dim.RemainingPercent()
			assert.InDelta(t, tt.expected, got, 0.0001)
		})
	}
}

func TestDimension_IsExhausted(t *testing.T) {
	tests := []struct {
		name     string
		dim      Dimension
		expected bool
	}{
		{
			name:     "exhausted at 0",
			dim:      Dimension{Limit: 100, Remaining: 0},
			expected: true,
		},
		{
			name:     "exhausted negative",
			dim:      Dimension{Limit: 100, Remaining: -1},
			expected: true,
		},
		{
			name:     "not exhausted",
			dim:      Dimension{Limit: 100, Remaining: 1},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dim.IsExhausted()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDimension_IsCritical(t *testing.T) {
	dim := Dimension{Limit: 100, Remaining: 10} // 10%

	tests := []struct {
		name      string
		threshold float64
		expected  bool
	}{
		// IsCritical returns true if RemainingPercent() < threshold
		// 10% < 5% = false, so not critical
		{"below threshold (5%)", 5.0, false},
		// 10% < 10% = false, at threshold is not critical
		{"at threshold (10%)", 10.0, false},
		// 10% < 15% = true, so critical
		{"above threshold (15%)", 15.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dim.IsCritical(tt.threshold)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDimension_TimeUntilReset(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	tests := []struct {
		name     string
		resetAt  *time.Time
		expected time.Duration
	}{
		{
			name:     "future reset",
			resetAt:  &future,
			expected: time.Hour,
		},
		{
			name:     "past reset",
			resetAt:  &past,
			expected: 0,
		},
		{
			name:     "no reset time",
			resetAt:  nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dim := Dimension{ResetAt: tt.resetAt}
			got := dim.TimeUntilReset()
			if tt.expected == 0 {
				assert.Equal(t, time.Duration(0), got)
			} else {
				assert.InDelta(t, tt.expected, got, float64(time.Second))
			}
		})
	}
}

func TestDimensionSlice_FindByType(t *testing.T) {
	ds := DimensionSlice{
		{Type: DimensionRPM, Limit: 100, Remaining: 50},
		{Type: DimensionTPM, Limit: 10000, Remaining: 5000},
		{Type: DimensionRPD, Limit: 1000, Remaining: 500},
	}

	tests := []struct {
		name       string
		searchType DimensionType
		wantFound  bool
		wantLimit  int64
	}{
		{"find RPM", DimensionRPM, true, 100},
		{"find TPM", DimensionTPM, true, 10000},
		{"find unknown", DimensionBudget, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dim, found := ds.FindByType(tt.searchType)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantLimit, dim.Limit)
			}
		})
	}
}

func TestDimensionSlice_MinRemainingPercent(t *testing.T) {
	tests := []struct {
		name     string
		ds       DimensionSlice
		expected float64
	}{
		{
			name:     "empty slice",
			ds:       DimensionSlice{},
			expected: 0,
		},
		{
			name: "single dimension",
			ds: DimensionSlice{
				{Limit: 100, Remaining: 25},
			},
			expected: 25.0,
		},
		{
			name: "multiple dimensions",
			ds: DimensionSlice{
				{Limit: 100, Remaining: 50}, // 50%
				{Limit: 100, Remaining: 25}, // 25% - min
				{Limit: 100, Remaining: 75}, // 75%
			},
			expected: 25.0,
		},
		{
			name: "all exhausted",
			ds: DimensionSlice{
				{Limit: 100, Remaining: 0},
				{Limit: 100, Remaining: 0},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ds.MinRemainingPercent()
			assert.InDelta(t, tt.expected, got, 0.0001)
		})
	}
}

func TestDimensionSlice_CriticalDimension(t *testing.T) {
	tests := []struct {
		name         string
		ds           DimensionSlice
		expectedType DimensionType
		expectedNil  bool
	}{
		{
			name:        "empty slice",
			ds:          DimensionSlice{},
			expectedNil: true,
		},
		{
			name: "single dimension",
			ds: DimensionSlice{
				{Type: DimensionRPM, Limit: 100, Remaining: 50},
			},
			expectedType: DimensionRPM,
		},
		{
			name: "finds minimum",
			ds: DimensionSlice{
				{Type: DimensionRPM, Limit: 100, Remaining: 50},
				{Type: DimensionTPM, Limit: 100, Remaining: 10}, // critical
				{Type: DimensionRPD, Limit: 100, Remaining: 80},
			},
			expectedType: DimensionTPM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dim := tt.ds.CriticalDimension()
			if tt.expectedNil {
				assert.Nil(t, dim)
			} else {
				require.NotNil(t, dim)
				assert.Equal(t, tt.expectedType, dim.Type)
			}
		})
	}
}

func TestDimension_JSON(t *testing.T) {
	resetAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	dim := Dimension{
		Type:       DimensionRPM,
		Limit:      100,
		Remaining:  50,
		ResetAt:    &resetAt,
		RefillRate: 10.5,
		Semantics:  WindowToken,
		Source:     SourceHeaders,
		Confidence: 0.95,
	}

	// Test marshal/unmarshal
	data, err := json.Marshal(dim)
	require.NoError(t, err)

	var decoded Dimension
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, dim.Type, decoded.Type)
	assert.Equal(t, dim.Limit, decoded.Limit)
	assert.Equal(t, dim.Remaining, decoded.Remaining)
	assert.Equal(t, dim.RefillRate, decoded.RefillRate)
	assert.Equal(t, dim.Semantics, decoded.Semantics)
	assert.Equal(t, dim.Source, decoded.Source)
	assert.Equal(t, dim.Confidence, decoded.Confidence)
	require.NotNil(t, decoded.ResetAt)
	assert.True(t, dim.ResetAt.Equal(*decoded.ResetAt))
}
