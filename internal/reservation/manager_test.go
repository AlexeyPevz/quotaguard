package reservation

import (
	"context"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	m := NewManager(s, cfg)

	assert.NotNil(t, m)
	assert.Equal(t, s, m.store)
	assert.Equal(t, cfg.DefaultTTL, m.defaultTTL)
}

func TestManager_Create(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup account with quota
	accountID := "acc-1"
	quota := &models.QuotaInfo{
		AccountID:             accountID,
		EffectiveRemainingPct: 80.0,
	}
	s.SetQuota(accountID, quota)

	t.Run("successful creation", func(t *testing.T) {
		res, err := m.Create(context.Background(), accountID, 10.0, "corr-1")
		require.NoError(t, err)
		assert.NotEmpty(t, res.ID)
		assert.Equal(t, accountID, res.AccountID)
		assert.Equal(t, 10.0, res.EstimatedCostPct)
		assert.Equal(t, models.ReservationActive, res.Status)
		assert.Equal(t, "corr-1", res.CorrelationID)

		// Check virtual usage was added
		q, _ := s.GetQuota(accountID)
		assert.Equal(t, 10.0, q.VirtualUsedPercent)
	})

	t.Run("insufficient quota", func(t *testing.T) {
		// Reset quota
		quota.VirtualUsedPercent = 0
		s.SetQuota(accountID, quota)

		// Try to reserve more than available
		_, err := m.Create(context.Background(), accountID, 90.0, "corr-2")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient quota")
	})

	t.Run("no quota data", func(t *testing.T) {
		_, err := m.Create(context.Background(), "non-existent", 10.0, "corr-3")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no quota data")
	})

	t.Run("invalid estimated cost", func(t *testing.T) {
		_, err := m.Create(context.Background(), accountID, -5.0, "corr-4")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be between 0 and 100")
	})
}

func TestManager_Get(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	res, _ := m.Create(context.Background(), accountID, 10.0, "corr-1")

	t.Run("existing reservation", func(t *testing.T) {
		found, ok := m.Get(res.ID)
		assert.True(t, ok)
		assert.Equal(t, res.ID, found.ID)
	})

	t.Run("non-existent reservation", func(t *testing.T) {
		_, ok := m.Get("non-existent")
		assert.False(t, ok)
	})
}

func TestManager_Release(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	res, _ := m.Create(context.Background(), accountID, 10.0, "corr-1")

	t.Run("successful release", func(t *testing.T) {
		err := m.Release(res.ID, 5.0)
		require.NoError(t, err)

		// Check status
		released, _ := m.Get(res.ID)
		assert.Equal(t, models.ReservationReleased, released.Status)
		assert.Equal(t, 5.0, *released.ActualCostPct)
		assert.NotNil(t, released.ReleasedAt)

		// Check virtual usage adjusted (10 removed, 5 added = 5 net)
		q, _ := s.GetQuota(accountID)
		assert.Equal(t, 5.0, q.VirtualUsedPercent)
	})

	t.Run("non-existent reservation", func(t *testing.T) {
		err := m.Release("non-existent", 5.0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("already released", func(t *testing.T) {
		err := m.Release(res.ID, 5.0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot release")
	})
}

func TestManager_Cancel(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	res, _ := m.Create(context.Background(), accountID, 10.0, "corr-1")

	t.Run("successful cancel", func(t *testing.T) {
		err := m.Cancel(res.ID)
		require.NoError(t, err)

		// Check status
		cancelled, _ := m.Get(res.ID)
		assert.Equal(t, models.ReservationCancelled, cancelled.Status)

		// Check virtual usage removed
		q, _ := s.GetQuota(accountID)
		assert.Equal(t, 0.0, q.VirtualUsedPercent)
	})

	t.Run("non-existent reservation", func(t *testing.T) {
		err := m.Cancel("non-existent")
		assert.Error(t, err)
	})
}

func TestManager_Expire(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	res, _ := m.Create(context.Background(), accountID, 10.0, "corr-1")

	t.Run("successful expire", func(t *testing.T) {
		err := m.Expire(res.ID)
		require.NoError(t, err)

		// Check status
		expired, _ := m.Get(res.ID)
		assert.Equal(t, models.ReservationExpired, expired.Status)

		// Check virtual usage removed
		q, _ := s.GetQuota(accountID)
		assert.Equal(t, 0.0, q.VirtualUsedPercent)
	})
}

func TestManager_CleanupExpired(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	// Create expired reservation manually
	expiredRes := &models.Reservation{
		ID:               "expired-1",
		AccountID:        accountID,
		EstimatedCostPct: 5.0,
		Status:           models.ReservationActive,
		CreatedAt:        time.Now().Add(-10 * time.Minute),
		ExpiresAt:        time.Now().Add(-5 * time.Minute),
		CorrelationID:    "corr-expired",
	}
	s.SetReservation(expiredRes.ID, expiredRes)

	// Add virtual usage
	quota.AddVirtualUsed(5.0)
	s.SetQuota(accountID, quota)

	t.Run("cleanup expired", func(t *testing.T) {
		count := m.CleanupExpired()
		assert.Equal(t, 1, count)

		// Check reservation expired
		res, _ := m.Get("expired-1")
		assert.Equal(t, models.ReservationExpired, res.Status)

		// Check virtual usage removed
		q, _ := s.GetQuota(accountID)
		assert.Equal(t, 0.0, q.VirtualUsedPercent)
	})
}

func TestManager_GetActiveByAccount(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	// Create reservations
	res1, err := m.Create(context.Background(), accountID, 10.0, "corr-1")
	require.NoError(t, err)
	res2, err := m.Create(context.Background(), accountID, 5.0, "corr-2")
	require.NoError(t, err)

	// Release one
	require.NoError(t, m.Release(res1.ID, 10.0))

	t.Run("get active", func(t *testing.T) {
		active := m.GetActiveByAccount(accountID)
		assert.Len(t, active, 1)
		assert.Equal(t, res2.ID, active[0].ID)
	})
}

func TestManager_GetTotalReservedPct(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	// Create reservations
	_, err := m.Create(context.Background(), accountID, 10.0, "corr-1")
	require.NoError(t, err)
	_, err = m.Create(context.Background(), accountID, 5.0, "corr-2")
	require.NoError(t, err)

	t.Run("total reserved", func(t *testing.T) {
		total := m.GetTotalReservedPct(accountID)
		assert.Equal(t, 15.0, total)
	})
}

func TestManager_GetMetrics(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	// Create and release reservations
	res1, err := m.Create(context.Background(), accountID, 10.0, "corr-1")
	require.NoError(t, err)
	res2, err := m.Create(context.Background(), accountID, 5.0, "corr-2")
	require.NoError(t, err)
	require.NoError(t, m.Release(res1.ID, 10.0))
	require.NoError(t, m.Cancel(res2.ID))

	t.Run("metrics", func(t *testing.T) {
		metrics := m.GetMetrics()
		assert.Equal(t, int64(2), metrics.CreatedTotal)
		assert.Equal(t, int64(1), metrics.ReleasedTotal)
		assert.Equal(t, int64(1), metrics.CancelledTotal)
		assert.Equal(t, int64(0), metrics.ActiveCount)
	})
}

func TestManager_StartCleanupRoutine(t *testing.T) {
	s := store.NewMemoryStore()
	m := NewManager(s, DefaultConfig())

	// Setup
	accountID := "acc-1"
	quota := &models.QuotaInfo{AccountID: accountID, EffectiveRemainingPct: 80.0}
	s.SetQuota(accountID, quota)

	// Create expired reservation
	expiredRes := &models.Reservation{
		ID:               "expired-1",
		AccountID:        accountID,
		EstimatedCostPct: 5.0,
		Status:           models.ReservationActive,
		CreatedAt:        time.Now().Add(-10 * time.Minute),
		ExpiresAt:        time.Now().Add(-5 * time.Minute),
		CorrelationID:    "corr-expired",
	}
	s.SetReservation(expiredRes.ID, expiredRes)
	quota.AddVirtualUsed(5.0)
	s.SetQuota(accountID, quota)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup routine with short interval
	m.StartCleanupRoutine(ctx, 100*time.Millisecond)

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	// Check reservation was expired
	res, _ := m.Get("expired-1")
	assert.Equal(t, models.ReservationExpired, res.Status)
}
