package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthStatus_Validate(t *testing.T) {
	tests := []struct {
		name    string
		health  HealthStatus
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid health status",
			health: HealthStatus{
				AccountID:       "acc-1",
				ErrorRate:       0.05,
				ShadowBanRisk:   0.1,
				BaselineLatency: time.Millisecond * 100,
				CurrentLatency:  time.Millisecond * 150,
			},
			wantErr: false,
		},
		{
			name: "missing account ID",
			health: HealthStatus{
				ErrorRate: 0.05,
			},
			wantErr: true,
			errMsg:  "account ID is required",
		},
		{
			name: "error rate too high",
			health: HealthStatus{
				AccountID: "acc-1",
				ErrorRate: 1.5,
			},
			wantErr: true,
			errMsg:  "error rate must be between 0 and 1",
		},
		{
			name: "error rate negative",
			health: HealthStatus{
				AccountID: "acc-1",
				ErrorRate: -0.1,
			},
			wantErr: true,
			errMsg:  "error rate must be between 0 and 1",
		},
		{
			name: "shadow ban risk too high",
			health: HealthStatus{
				AccountID:     "acc-1",
				ShadowBanRisk: 1.5,
			},
			wantErr: true,
			errMsg:  "shadow ban risk must be between 0 and 1",
		},
		{
			name: "negative baseline latency",
			health: HealthStatus{
				AccountID:       "acc-1",
				BaselineLatency: -time.Millisecond,
			},
			wantErr: true,
			errMsg:  "baseline latency cannot be negative",
		},
		{
			name: "negative current latency",
			health: HealthStatus{
				AccountID:      "acc-1",
				CurrentLatency: -time.Millisecond,
			},
			wantErr: true,
			errMsg:  "current latency cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.health.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHealthStatus_IsHealthy(t *testing.T) {
	tests := []struct {
		name           string
		errorRate      float64
		isShadowBanned bool
		threshold      float64
		expected       bool
	}{
		{"healthy", 0.05, false, 0.1, true},
		{"at threshold", 0.1, false, 0.1, false},
		{"above threshold", 0.15, false, 0.1, false},
		{"shadow banned", 0.05, true, 0.1, false},
		{"both issues", 0.15, true, 0.1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := HealthStatus{
				AccountID:      "acc-1",
				ErrorRate:      tt.errorRate,
				IsShadowBanned: tt.isShadowBanned,
			}
			got := h.IsHealthy(tt.threshold)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestHealthStatus_LatencySpike(t *testing.T) {
	tests := []struct {
		name       string
		baseline   time.Duration
		current    time.Duration
		multiplier float64
		expected   bool
	}{
		{"no spike", time.Millisecond * 100, time.Millisecond * 150, 5.0, false},
		{"spike detected", time.Millisecond * 100, time.Millisecond * 600, 5.0, true},
		{"at threshold", time.Millisecond * 100, time.Millisecond * 500, 5.0, false},
		{"zero baseline", 0, time.Millisecond * 1000, 5.0, false},
		{"high multiplier", time.Millisecond * 100, time.Millisecond * 600, 10.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := HealthStatus{
				AccountID:       "acc-1",
				BaselineLatency: tt.baseline,
				CurrentLatency:  tt.current,
			}
			got := h.LatencySpike(tt.multiplier)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestHealthStatus_UpdateErrorRate(t *testing.T) {
	tests := []struct {
		name              string
		successful        int64
		failed            int64
		expectedErrorRate float64
	}{
		{"no requests", 0, 0, 0},
		{"all successful", 100, 0, 0},
		{"all failed", 0, 100, 1.0},
		{"50/50", 50, 50, 0.5},
		{"25% error rate", 75, 25, 0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := HealthStatus{
				AccountID:          "acc-1",
				SuccessfulRequests: tt.successful,
				FailedRequests:     tt.failed,
			}
			h.UpdateErrorRate()
			assert.InDelta(t, tt.expectedErrorRate, h.ErrorRate, 0.0001)
		})
	}
}

func TestHealthStatus_RecordSuccess(t *testing.T) {
	h := HealthStatus{
		AccountID:          "acc-1",
		SuccessfulRequests: 10,
		FailedRequests:     2,
		ConsecutiveErrors:  3,
	}

	h.RecordSuccess(time.Millisecond * 200)

	// RecordSuccess increments SuccessfulRequests and TotalRequests
	assert.Equal(t, int64(11), h.SuccessfulRequests)
	// TotalRequests is incremented directly (not calculated from existing values)
	// Starting from 0, incremented once -> 1
	assert.Equal(t, int64(1), h.TotalRequests)
	assert.Equal(t, time.Millisecond*200, h.CurrentLatency)
	assert.Equal(t, 0, h.ConsecutiveErrors)
	// Error rate is calculated by UpdateErrorRate: Failed / (Successful + Failed)
	// After RecordSuccess: Successful=11, Failed=2, Total=1 (but UpdateErrorRate uses Successful+Failed)
	// So ErrorRate = 2 / (11 + 2) = 2/13
	assert.InDelta(t, 2.0/13.0, h.ErrorRate, 0.0001)
	assert.False(t, h.LastCheckedAt.IsZero())
}

func TestHealthStatus_RecordFailure(t *testing.T) {
	h := HealthStatus{
		AccountID:          "acc-1",
		SuccessfulRequests: 10,
		FailedRequests:     2,
		ConsecutiveErrors:  1,
	}

	h.RecordFailure()

	// RecordFailure increments FailedRequests and TotalRequests
	assert.Equal(t, int64(3), h.FailedRequests)
	// TotalRequests is incremented directly (not calculated from existing values)
	// Starting from 0, incremented once -> 1
	assert.Equal(t, int64(1), h.TotalRequests)
	assert.Equal(t, 2, h.ConsecutiveErrors)
	// Error rate is calculated by UpdateErrorRate: Failed / (Successful + Failed)
	// After RecordFailure: Successful=10, Failed=3
	// So ErrorRate = 3 / (10 + 3) = 3/13
	assert.InDelta(t, 3.0/13.0, h.ErrorRate, 0.0001)
	assert.False(t, h.LastCheckedAt.IsZero())
}

func TestHealthStatus_UpdateShadowBanRisk(t *testing.T) {
	tests := []struct {
		name              string
		errorRate         float64
		consecutiveErrors int
		expectedRisk      float64
		expectedBanned    bool
	}{
		{"no issues", 0, 0, 0, false},
		{"high error rate", 0.5, 0, 0.25, false},
		{"some consecutive errors", 0, 6, 0.3, false},
		{"many consecutive errors", 0, 11, 0.5, false},
		{"both factors", 0.5, 11, 0.75, false},
		{"critical", 1.0, 20, 1.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := HealthStatus{
				AccountID:         "acc-1",
				ErrorRate:         tt.errorRate,
				ConsecutiveErrors: tt.consecutiveErrors,
			}
			h.UpdateShadowBanRisk()
			assert.InDelta(t, tt.expectedRisk, h.ShadowBanRisk, 0.0001)
			assert.Equal(t, tt.expectedBanned, h.IsShadowBanned)
		})
	}
}

func TestHealthStatusSlice_FindByAccountID(t *testing.T) {
	statuses := HealthStatusSlice{
		{AccountID: "acc-1", ErrorRate: 0.05},
		{AccountID: "acc-2", ErrorRate: 0.1},
		{AccountID: "acc-3", ErrorRate: 0.02},
	}

	tests := []struct {
		name      string
		id        string
		wantFound bool
		wantRate  float64
	}{
		{"find acc-1", "acc-1", true, 0.05},
		{"find acc-2", "acc-2", true, 0.1},
		{"find unknown", "acc-999", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, found := statuses.FindByAccountID(tt.id)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.InDelta(t, tt.wantRate, h.ErrorRate, 0.0001)
			}
		})
	}
}

func TestHealthStatusSlice_FilterHealthy(t *testing.T) {
	statuses := HealthStatusSlice{
		{AccountID: "acc-1", ErrorRate: 0.05, IsShadowBanned: false},
		{AccountID: "acc-2", ErrorRate: 0.15, IsShadowBanned: false},
		{AccountID: "acc-3", ErrorRate: 0.02, IsShadowBanned: true},
		{AccountID: "acc-4", ErrorRate: 0.08, IsShadowBanned: false},
	}

	filtered := statuses.FilterHealthy(0.1)

	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-1", filtered[0].AccountID)
	assert.Equal(t, "acc-4", filtered[1].AccountID)
}

func TestHealthStatusSlice_FilterUnhealthy(t *testing.T) {
	statuses := HealthStatusSlice{
		{AccountID: "acc-1", ErrorRate: 0.05, IsShadowBanned: false},
		{AccountID: "acc-2", ErrorRate: 0.15, IsShadowBanned: false},
		{AccountID: "acc-3", ErrorRate: 0.02, IsShadowBanned: true},
		{AccountID: "acc-4", ErrorRate: 0.08, IsShadowBanned: false},
	}

	filtered := statuses.FilterUnhealthy(0.1)

	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-2", filtered[0].AccountID)
	assert.Equal(t, "acc-3", filtered[1].AccountID)
}

func TestHealthStatus_JSON(t *testing.T) {
	checkedAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	h := HealthStatus{
		AccountID:          "acc-1",
		LastCheckedAt:      checkedAt,
		BaselineLatency:    time.Millisecond * 100,
		CurrentLatency:     time.Millisecond * 150,
		ErrorRate:          0.05,
		ShadowBanRisk:      0.1,
		IsShadowBanned:     false,
		ConsecutiveErrors:  2,
		SuccessfulRequests: 95,
		FailedRequests:     5,
		TotalRequests:      100,
	}

	// Test marshal/unmarshal
	data, err := json.Marshal(h)
	require.NoError(t, err)

	var decoded HealthStatus
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, h.AccountID, decoded.AccountID)
	assert.True(t, h.LastCheckedAt.Equal(decoded.LastCheckedAt))
	assert.Equal(t, h.BaselineLatency, decoded.BaselineLatency)
	assert.Equal(t, h.CurrentLatency, decoded.CurrentLatency)
	assert.InDelta(t, h.ErrorRate, decoded.ErrorRate, 0.0001)
	assert.InDelta(t, h.ShadowBanRisk, decoded.ShadowBanRisk, 0.0001)
	assert.Equal(t, h.IsShadowBanned, decoded.IsShadowBanned)
	assert.Equal(t, h.ConsecutiveErrors, decoded.ConsecutiveErrors)
	assert.Equal(t, h.SuccessfulRequests, decoded.SuccessfulRequests)
	assert.Equal(t, h.FailedRequests, decoded.FailedRequests)
	assert.Equal(t, h.TotalRequests, decoded.TotalRequests)
}
