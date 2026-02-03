package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockQuotaFetcher is a mock implementation for testing
type MockQuotaFetcher struct {
	fetchFunc func(ctx context.Context, accountID string) (*models.QuotaInfo, error)
}

func (m *MockQuotaFetcher) FetchQuota(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
	if m.fetchFunc != nil {
		return m.fetchFunc(ctx, accountID)
	}
	return &models.QuotaInfo{
		AccountID:             accountID,
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 50.0,
	}, nil
}

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Minute, nil)
	require.NotNil(t, cb)
	assert.Equal(t, 3, cb.failureThreshold)
	assert.Equal(t, 5*time.Minute, cb.timeout)
	assert.Equal(t, CircuitClosed, cb.state)
}

func TestCircuitBreaker_Allow(t *testing.T) {
	t.Run("closed circuit allows requests", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 5*time.Minute, nil)
		assert.True(t, cb.Allow())
	})

	t.Run("open circuit blocks requests", func(t *testing.T) {
		cb := NewCircuitBreaker(2, 5*time.Minute, nil)
		cb.RecordFailure()
		cb.RecordFailure()
		assert.Equal(t, CircuitOpen, cb.State())
		assert.False(t, cb.Allow())
	})

	t.Run("half-open after timeout", func(t *testing.T) {
		cb := NewCircuitBreaker(1, 50*time.Millisecond, nil)
		cb.RecordFailure()
		assert.Equal(t, CircuitOpen, cb.State())

		time.Sleep(100 * time.Millisecond)
		assert.True(t, cb.Allow())
		assert.Equal(t, CircuitHalfOpen, cb.State())
	})
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker(2, 5*time.Minute, nil)
	cb.RecordFailure()
	cb.RecordFailure()
	assert.Equal(t, CircuitOpen, cb.State())

	cb.RecordSuccess()
	assert.Equal(t, CircuitClosed, cb.State())
	assert.Equal(t, 0, cb.failures)
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	t.Run("failure count increments", func(t *testing.T) {
		cb := NewCircuitBreaker(3, 5*time.Minute, nil)
		cb.RecordFailure()
		assert.Equal(t, 1, cb.failures)
		assert.Equal(t, CircuitClosed, cb.State())
	})

	t.Run("circuit opens after threshold", func(t *testing.T) {
		cb := NewCircuitBreaker(2, 5*time.Minute, nil)
		cb.RecordFailure()
		cb.RecordFailure()
		assert.Equal(t, CircuitOpen, cb.State())
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 60*time.Second, cfg.Interval)
	assert.True(t, cfg.Adaptive)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryBackoff)
	assert.True(t, cfg.CBEnabled)
	assert.Equal(t, 3, cfg.CBThreshold)
	assert.Equal(t, 5*time.Minute, cfg.CBTimeout)
}

func TestNewActiveCollector(t *testing.T) {
	s := store.NewMemoryStore()
	fetcher := &MockQuotaFetcher{}
	cfg := DefaultConfig()

	ac := NewActiveCollector(s, fetcher, cfg, nil)
	require.NotNil(t, ac)
	assert.Equal(t, s, ac.store)
	assert.Equal(t, fetcher, ac.fetcher)
	assert.Equal(t, cfg.Interval, ac.interval)
	assert.True(t, ac.cbEnabled)
	assert.NotNil(t, ac.cb)
}

func TestActiveCollector_StartStop(t *testing.T) {
	s := store.NewMemoryStore()
	fetcher := &MockQuotaFetcher{}
	cfg := DefaultConfig()
	cfg.Interval = 100 * time.Millisecond

	ac := NewActiveCollector(s, fetcher, cfg, nil)
	ctx := context.Background()

	t.Run("start and stop", func(t *testing.T) {
		err := ac.Start(ctx)
		require.NoError(t, err)
		assert.True(t, ac.IsRunning())

		err = ac.Stop()
		require.NoError(t, err)
		assert.False(t, ac.IsRunning())
	})

	t.Run("double start", func(t *testing.T) {
		err := ac.Start(ctx)
		require.NoError(t, err)

		err = ac.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")

		if err := ac.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
	})

	t.Run("stop when not running", func(t *testing.T) {
		err := ac.Stop()
		require.NoError(t, err)
	})
}

func TestActiveCollector_Poll(t *testing.T) {
	s := store.NewMemoryStore()

	// Add test accounts
	acc1 := &models.Account{ID: "acc-1", Provider: "openai", Enabled: true}
	acc2 := &models.Account{ID: "acc-2", Provider: "anthropic", Enabled: true}
	s.SetAccount(acc1)
	s.SetAccount(acc2)

	callCount := 0
	fetcher := &MockQuotaFetcher{
		fetchFunc: func(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
			callCount++
			return &models.QuotaInfo{
				AccountID:             accountID,
				Provider:              models.ProviderOpenAI,
				EffectiveRemainingPct: 50.0,
			}, nil
		},
	}

	cfg := DefaultConfig()
	cfg.Interval = 100 * time.Millisecond
	ac := NewActiveCollector(s, fetcher, cfg, nil)

	ctx := context.Background()
	err := ac.Start(ctx)
	require.NoError(t, err)

	// Wait for at least one poll
	time.Sleep(150 * time.Millisecond)

	err = ac.Stop()
	require.NoError(t, err)

	// Should have fetched quotas for both accounts
	assert.GreaterOrEqual(t, callCount, 2)

	// Check that quotas were stored
	_, ok := s.GetQuota("acc-1")
	assert.True(t, ok)
	_, ok = s.GetQuota("acc-2")
	assert.True(t, ok)
}

func TestActiveCollector_FetchWithRetry(t *testing.T) {
	s := store.NewMemoryStore()

	t.Run("success on first attempt", func(t *testing.T) {
		fetcher := &MockQuotaFetcher{
			fetchFunc: func(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
				return &models.QuotaInfo{AccountID: accountID}, nil
			},
		}

		cfg := DefaultConfig()
		ac := NewActiveCollector(s, fetcher, cfg, nil)

		quota, err := ac.fetchWithRetry(context.Background(), "acc-1")
		require.NoError(t, err)
		assert.Equal(t, "acc-1", quota.AccountID)
	})

	t.Run("success after retries", func(t *testing.T) {
		attemptCount := 0
		fetcher := &MockQuotaFetcher{
			fetchFunc: func(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
				attemptCount++
				if attemptCount < 3 {
					return nil, errors.New("temporary error")
				}
				return &models.QuotaInfo{AccountID: accountID}, nil
			},
		}

		cfg := DefaultConfig()
		cfg.RetryAttempts = 3
		cfg.RetryBackoff = 10 * time.Millisecond
		ac := NewActiveCollector(s, fetcher, cfg, nil)

		quota, err := ac.fetchWithRetry(context.Background(), "acc-1")
		require.NoError(t, err)
		assert.Equal(t, "acc-1", quota.AccountID)
		assert.Equal(t, 3, attemptCount)
	})

	t.Run("failure after all retries", func(t *testing.T) {
		fetcher := &MockQuotaFetcher{
			fetchFunc: func(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
				return nil, errors.New("persistent error")
			},
		}

		cfg := DefaultConfig()
		cfg.RetryAttempts = 2
		cfg.RetryBackoff = 10 * time.Millisecond
		ac := NewActiveCollector(s, fetcher, cfg, nil)

		_, err := ac.fetchWithRetry(context.Background(), "acc-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed after 3 attempts")
	})
}

func TestActiveCollector_AdaptiveInterval(t *testing.T) {
	s := store.NewMemoryStore()
	fetcher := &MockQuotaFetcher{}

	cfg := DefaultConfig()
	cfg.Interval = 60 * time.Second
	cfg.Adaptive = true
	ac := NewActiveCollector(s, fetcher, cfg, nil)

	// Test critical level (< 20%)
	s.SetAccount(&models.Account{ID: "acc-1", Enabled: true})
	s.SetQuota("acc-1", &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 10.0,
	})
	ac.updateAdaptiveInterval([]*models.Account{{ID: "acc-1", Enabled: true}})
	assert.Equal(t, 15*time.Second, ac.GetInterval())

	// Test warning level (20-50%)
	s.SetQuota("acc-1", &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 30.0,
	})
	ac.updateAdaptiveInterval([]*models.Account{{ID: "acc-1", Enabled: true}})
	assert.Equal(t, 30*time.Second, ac.GetInterval())

	// Test normal level (50-80%)
	s.SetQuota("acc-1", &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 60.0,
	})
	ac.updateAdaptiveInterval([]*models.Account{{ID: "acc-1", Enabled: true}})
	assert.Equal(t, 60*time.Second, ac.GetInterval())

	// Test healthy level (> 80%)
	s.SetQuota("acc-1", &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 90.0,
	})
	ac.updateAdaptiveInterval([]*models.Account{{ID: "acc-1", Enabled: true}})
	assert.Equal(t, 120*time.Second, ac.GetInterval())
}

func TestActiveCollector_CircuitBreaker(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{ID: "acc-1", Enabled: true})

	// Fail all requests
	fetcher := &MockQuotaFetcher{
		fetchFunc: func(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
			return nil, errors.New("connection error")
		},
	}

	cfg := DefaultConfig()
	cfg.Interval = 50 * time.Millisecond
	cfg.CBThreshold = 2
	cfg.CBTimeout = 100 * time.Millisecond
	cfg.RetryAttempts = 0
	cfg.RetryBackoff = time.Millisecond
	ac := NewActiveCollector(s, fetcher, cfg, nil)

	ctx := context.Background()
	err := ac.Start(ctx)
	require.NoError(t, err)

	// Wait for failures to trigger circuit breaker
	time.Sleep(250 * time.Millisecond)

	// Circuit should be open
	assert.Equal(t, CircuitOpen, ac.GetCircuitBreakerState())

	if err := ac.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestActiveCollector_GetCircuitBreakerState_Disabled(t *testing.T) {
	s := store.NewMemoryStore()
	fetcher := &MockQuotaFetcher{}

	cfg := DefaultConfig()
	cfg.CBEnabled = false
	ac := NewActiveCollector(s, fetcher, cfg, nil)

	// Should return CircuitClosed when disabled
	assert.Equal(t, CircuitClosed, ac.GetCircuitBreakerState())
}
