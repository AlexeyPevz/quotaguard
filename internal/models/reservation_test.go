package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReservation_Validate(t *testing.T) {
	tests := []struct {
		name    string
		res     Reservation
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid reservation",
			res: Reservation{
				ID:               "res-1",
				AccountID:        "acc-1",
				EstimatedCostPct: 5.0,
				Status:           ReservationActive,
				CorrelationID:    "corr-1",
				CreatedAt:        time.Now(),
				ExpiresAt:        time.Now().Add(time.Minute),
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			res: Reservation{
				AccountID:        "acc-1",
				EstimatedCostPct: 5.0,
				Status:           ReservationActive,
				CorrelationID:    "corr-1",
			},
			wantErr: true,
			errMsg:  "reservation ID is required",
		},
		{
			name: "missing account ID",
			res: Reservation{
				ID:               "res-1",
				EstimatedCostPct: 5.0,
				Status:           ReservationActive,
				CorrelationID:    "corr-1",
			},
			wantErr: true,
			errMsg:  "account ID is required",
		},
		{
			name: "negative estimated cost",
			res: Reservation{
				ID:               "res-1",
				AccountID:        "acc-1",
				EstimatedCostPct: -1.0,
				Status:           ReservationActive,
				CorrelationID:    "corr-1",
			},
			wantErr: true,
			errMsg:  "estimated cost cannot be negative",
		},
		{
			name: "estimated cost exceeds 100",
			res: Reservation{
				ID:               "res-1",
				AccountID:        "acc-1",
				EstimatedCostPct: 101.0,
				Status:           ReservationActive,
				CorrelationID:    "corr-1",
			},
			wantErr: true,
			errMsg:  "estimated cost cannot exceed 100",
		},
		{
			name: "missing status",
			res: Reservation{
				ID:               "res-1",
				AccountID:        "acc-1",
				EstimatedCostPct: 5.0,
				CorrelationID:    "corr-1",
			},
			wantErr: true,
			errMsg:  "status is required",
		},
		{
			name: "missing correlation ID",
			res: Reservation{
				ID:               "res-1",
				AccountID:        "acc-1",
				EstimatedCostPct: 5.0,
				Status:           ReservationActive,
			},
			wantErr: true,
			errMsg:  "correlation ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.res.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestReservation_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		expiresAt time.Time
		expected  bool
	}{
		{
			name:      "not expired",
			expiresAt: now.Add(time.Hour),
			expected:  false,
		},
		{
			name:      "expired",
			expiresAt: now.Add(-time.Hour),
			expected:  true,
		},
		{
			name:      "just expired",
			expiresAt: now.Add(-time.Second),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Reservation{ExpiresAt: tt.expiresAt}
			got := r.IsExpired()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestReservation_IsActive(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	tests := []struct {
		name      string
		status    ReservationStatus
		expiresAt time.Time
		expected  bool
	}{
		{"active and not expired", ReservationActive, future, true},
		{"active but expired", ReservationActive, past, false},
		{"pending", ReservationPending, future, false},
		{"released", ReservationReleased, future, false},
		{"expired status", ReservationExpired, future, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Reservation{Status: tt.status, ExpiresAt: tt.expiresAt}
			got := r.IsActive()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestReservation_CanRelease(t *testing.T) {
	tests := []struct {
		name     string
		status   ReservationStatus
		expected bool
	}{
		{"active", ReservationActive, true},
		{"pending", ReservationPending, true},
		{"released", ReservationReleased, false},
		{"expired", ReservationExpired, false},
		{"cancelled", ReservationCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Reservation{Status: tt.status}
			got := r.CanRelease()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestReservation_Release(t *testing.T) {
	tests := []struct {
		name    string
		status  ReservationStatus
		wantErr bool
		errMsg  string
	}{
		{"release active", ReservationActive, false, ""},
		{"release pending", ReservationPending, false, ""},
		{"release released", ReservationReleased, true, "cannot release reservation with status released"},
		{"release expired", ReservationExpired, true, "cannot release reservation with status expired"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Reservation{
				ID:               "res-1",
				AccountID:        "acc-1",
				EstimatedCostPct: 5.0,
				Status:           tt.status,
				CreatedAt:        time.Now(),
				ExpiresAt:        time.Now().Add(time.Minute),
				CorrelationID:    "corr-1",
			}

			err := r.Release(3.5)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, ReservationReleased, r.Status)
				require.NotNil(t, r.ReleasedAt)
				require.NotNil(t, r.ActualCostPct)
				assert.InDelta(t, 3.5, *r.ActualCostPct, 0.0001)
			}
		})
	}
}

func TestReservation_Cancel(t *testing.T) {
	tests := []struct {
		name    string
		status  ReservationStatus
		wantErr bool
	}{
		{"cancel active", ReservationActive, false},
		{"cancel pending", ReservationPending, false},
		{"cancel released", ReservationReleased, true},
		{"cancel expired", ReservationExpired, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Reservation{Status: tt.status}
			err := r.Cancel()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, ReservationCancelled, r.Status)
			}
		})
	}
}

func TestReservation_Expire(t *testing.T) {
	tests := []struct {
		name    string
		status  ReservationStatus
		wantErr bool
	}{
		{"expire active", ReservationActive, false},
		{"expire pending", ReservationPending, false},
		{"expire released", ReservationReleased, true},
		{"expire expired", ReservationExpired, true},
		{"expire cancelled", ReservationCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Reservation{Status: tt.status}
			err := r.Expire()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, ReservationExpired, r.Status)
			}
		})
	}
}

func TestReservation_TimeUntilExpiry(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		expiresAt time.Time
		expected  time.Duration
	}{
		{
			name:      "future expiry",
			expiresAt: now.Add(time.Hour),
			expected:  time.Hour,
		},
		{
			name:      "past expiry",
			expiresAt: now.Add(-time.Hour),
			expected:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Reservation{ExpiresAt: tt.expiresAt}
			got := r.TimeUntilExpiry()
			if tt.expected == 0 {
				assert.Equal(t, time.Duration(0), got)
			} else {
				assert.InDelta(t, tt.expected, got, float64(time.Second))
			}
		})
	}
}

func TestReservation_Age(t *testing.T) {
	createdAt := time.Now().Add(-time.Hour)
	r := Reservation{CreatedAt: createdAt}

	age := r.Age()
	assert.InDelta(t, time.Hour, age, float64(time.Second))
}

func TestReservationSlice_FilterActive(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	reservations := ReservationSlice{
		{ID: "res-1", Status: ReservationActive, ExpiresAt: future},
		{ID: "res-2", Status: ReservationActive, ExpiresAt: past}, // expired
		{ID: "res-3", Status: ReservationPending, ExpiresAt: future},
		{ID: "res-4", Status: ReservationReleased, ExpiresAt: future},
	}

	filtered := reservations.FilterActive()

	assert.Len(t, filtered, 1)
	assert.Equal(t, "res-1", filtered[0].ID)
}

func TestReservationSlice_FilterByAccountID(t *testing.T) {
	reservations := ReservationSlice{
		{ID: "res-1", AccountID: "acc-1"},
		{ID: "res-2", AccountID: "acc-2"},
		{ID: "res-3", AccountID: "acc-1"},
	}

	filtered := reservations.FilterByAccountID("acc-1")

	assert.Len(t, filtered, 2)
	assert.Equal(t, "res-1", filtered[0].ID)
	assert.Equal(t, "res-3", filtered[1].ID)
}

func TestReservationSlice_TotalEstimatedCost(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	reservations := ReservationSlice{
		{ID: "res-1", Status: ReservationActive, ExpiresAt: future, EstimatedCostPct: 5.0},
		{ID: "res-2", Status: ReservationActive, ExpiresAt: future, EstimatedCostPct: 3.0},
		{ID: "res-3", Status: ReservationActive, ExpiresAt: past, EstimatedCostPct: 2.0}, // expired
		{ID: "res-4", Status: ReservationReleased, ExpiresAt: future, EstimatedCostPct: 4.0},
	}

	total := reservations.TotalEstimatedCost()

	assert.InDelta(t, 8.0, total, 0.0001) // 5.0 + 3.0
}

func TestReservationSlice_FindByID(t *testing.T) {
	reservations := ReservationSlice{
		{ID: "res-1", AccountID: "acc-1"},
		{ID: "res-2", AccountID: "acc-2"},
	}

	tests := []struct {
		name      string
		id        string
		wantFound bool
	}{
		{"find res-1", "res-1", true},
		{"find res-2", "res-2", true},
		{"find unknown", "res-999", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, found := reservations.FindByID(tt.id)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.id, r.ID)
			}
		})
	}
}

func TestReservation_JSON(t *testing.T) {
	createdAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)
	releasedAt := time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)
	actualCost := 3.5

	res := Reservation{
		ID:               "res-1",
		AccountID:        "acc-1",
		EstimatedCostPct: 5.0,
		Status:           ReservationReleased,
		CreatedAt:        createdAt,
		ExpiresAt:        expiresAt,
		ReleasedAt:       &releasedAt,
		CorrelationID:    "corr-1",
		ActualCostPct:    &actualCost,
	}

	// Test marshal/unmarshal
	data, err := json.Marshal(res)
	require.NoError(t, err)

	var decoded Reservation
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, res.ID, decoded.ID)
	assert.Equal(t, res.AccountID, decoded.AccountID)
	assert.Equal(t, res.EstimatedCostPct, decoded.EstimatedCostPct)
	assert.Equal(t, res.Status, decoded.Status)
	assert.Equal(t, res.CorrelationID, decoded.CorrelationID)
	assert.True(t, res.CreatedAt.Equal(decoded.CreatedAt))
	assert.True(t, res.ExpiresAt.Equal(decoded.ExpiresAt))
	require.NotNil(t, decoded.ReleasedAt)
	assert.True(t, res.ReleasedAt.Equal(*decoded.ReleasedAt))
	require.NotNil(t, decoded.ActualCostPct)
	assert.InDelta(t, *res.ActualCostPct, *decoded.ActualCostPct, 0.0001)
}
