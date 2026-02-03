package router

import (
	"context"
	"testing"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouterInterfaceImplementation tests that the router struct implements the Router interface
func TestRouterInterfaceImplementation(t *testing.T) {
	// This test verifies that *router implements Router interface
	// We use a type assertion to check if *router satisfies Router
	var _ Router = (*router)(nil)

	// Create a real router instance with memory store
	s := store.NewMemoryStore()
	// Add an account so CalculateOptimalDistribution doesn't return nil
	s.SetAccount(&models.Account{ID: "test-acc", Enabled: true})
	s.SetQuota("test-acc", &models.QuotaInfo{
		AccountID:             "test-acc",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
	})

	r := NewRouter(s, DefaultConfig())

	// Verify it implements all interface methods
	require.NotNil(t, r)

	// Test Select method
	resp, err := r.Select(context.Background(), SelectRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-acc", resp.AccountID)

	// Test GetAccounts
	accounts, err := r.GetAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 1)

	// Test GetQuota
	quota, err := r.GetQuota(context.Background(), "test-acc")
	require.NoError(t, err)
	require.NotNil(t, quota)
	assert.Equal(t, "test-acc", quota.AccountID)

	// Test GetAllQuotas
	quotas, err := r.GetAllQuotas(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, quotas)

	// Test GetRoutingDistribution
	dist, err := r.GetRoutingDistribution(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, dist)

	// Test CheckHealth
	_, err = r.CheckHealth(context.Background(), "test")
	assert.Error(t, err) // Should error for non-existent account

	// Test GetConfig
	config := r.GetConfig()
	require.NotNil(t, config)

	// Test Close
	err = r.Close()
	assert.NoError(t, err)

	// Test IsHealthy
	healthy := r.IsHealthy()
	assert.True(t, healthy) // Has one account, so healthy

	// Test RecordSwitch
	r.RecordSwitch("test-account")

	// Test GetCurrentAccount
	currentAccount := r.GetCurrentAccount()
	assert.Equal(t, "test-account", currentAccount)

	// Test GetAccountStatus - should error for non-existent account
	status, err := r.GetAccountStatus("non-existent")
	assert.Error(t, err)
	assert.Nil(t, status)

	// Test CalculateOptimalDistribution
	dist2 := r.CalculateOptimalDistribution(context.Background(), 100)
	assert.NotNil(t, dist2)

	// Test GetStats
	stats := r.GetStats()
	assert.NotNil(t, stats)

	_ = s // Suppress unused variable warning
}

// TestRouterInterfaceWithRealRouter tests that the router can be used via the interface with real data
func TestRouterInterfaceWithRealRouter(t *testing.T) {
	// Create a router and use it through the interface
	s := store.NewMemoryStore()

	// Add test account
	acc := &models.Account{
		ID:         "test-acc-1",
		Provider:   models.ProviderOpenAI,
		Enabled:    true,
		Priority:   10,
		InputCost:  0.01,
		OutputCost: 0.03,
	}
	s.SetAccount(acc)

	// Add quota
	quota := &models.QuotaInfo{
		AccountID:             "test-acc-1",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
		Confidence:            0.95,
	}
	s.SetQuota("test-acc-1", quota)

	var r Router = NewRouter(s, DefaultConfig())

	// Use the interface
	accounts, err := r.GetAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 1)

	quotaResult, err := r.GetQuota(context.Background(), "test-acc-1")
	require.NoError(t, err)
	require.NotNil(t, quotaResult)
	assert.Equal(t, 80.0, quotaResult.EffectiveRemainingPct)

	// Test Select
	resp, err := r.Select(context.Background(), SelectRequest{
		Provider: models.ProviderOpenAI,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-acc-1", resp.AccountID)

	// Test IsHealthy
	assert.True(t, r.IsHealthy())

	_ = s // Suppress unused variable warning
}

// TestRouterInterfaceWithMultipleAccounts tests router with multiple accounts
func TestRouterInterfaceWithMultipleAccounts(t *testing.T) {
	s := store.NewMemoryStore()

	// Add multiple accounts
	accounts := []*models.Account{
		{
			ID:         "high-priority",
			Provider:   models.ProviderOpenAI,
			Enabled:    true,
			Priority:   10,
			InputCost:  0.01,
			OutputCost: 0.03,
		},
		{
			ID:         "low-priority",
			Provider:   models.ProviderOpenAI,
			Enabled:    true,
			Priority:   5,
			InputCost:  0.015,
			OutputCost: 0.045,
		},
	}

	for _, acc := range accounts {
		s.SetAccount(acc)
	}

	// Add quotas
	quotas := map[string]*models.QuotaInfo{
		"high-priority": {
			AccountID:             "high-priority",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 80.0,
			Confidence:            0.95,
		},
		"low-priority": {
			AccountID:             "low-priority",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 60.0,
			Confidence:            0.90,
		},
	}

	for id, quota := range quotas {
		s.SetQuota(id, quota)
	}

	var r Router = NewRouter(s, DefaultConfig())

	// Test Select - should choose high priority with higher quota
	resp, err := r.Select(context.Background(), SelectRequest{
		Provider: models.ProviderOpenAI,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "high-priority", resp.AccountID)
	assert.Greater(t, resp.Score, 0.0)

	// Test GetAllQuotas
	allQuotas, err := r.GetAllQuotas(context.Background())
	require.NoError(t, err)
	assert.Len(t, allQuotas, 2)

	// Test CalculateOptimalDistribution
	dist := r.CalculateOptimalDistribution(context.Background(), 100)
	require.NotNil(t, dist)
	assert.Len(t, dist, 2)

	// High priority should have higher distribution
	highDist := dist["high-priority"]
	lowDist := dist["low-priority"]
	assert.GreaterOrEqual(t, highDist, lowDist)

	_ = s // Suppress unused variable warning
}

// TestRouterInterfaceHealthCheck tests health check functionality
func TestRouterInterfaceHealthCheck(t *testing.T) {
	s := store.NewMemoryStore()

	var r Router = NewRouter(s, DefaultConfig())

	// Initially should not be healthy (no accounts)
	assert.False(t, r.IsHealthy())

	// Add an account
	acc := &models.Account{
		ID:       "healthy-acc",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
	}
	s.SetAccount(acc)

	// Now should be healthy
	assert.True(t, r.IsHealthy())

	// Test CheckHealth
	health, err := r.CheckHealth(context.Background(), "healthy-acc")
	require.NoError(t, err)
	require.NotNil(t, health)
	assert.Equal(t, "healthy-acc", health.AccountID)

	_ = s // Suppress unused variable warning
}

// TestRouterInterfaceConfig tests config functionality
func TestRouterInterfaceConfig(t *testing.T) {
	s := store.NewMemoryStore()

	cfg := DefaultConfig()
	cfg.SwitchThreshold = 15.0
	cfg.MinDwellTime = 10 // Changed value

	var r Router = NewRouter(s, cfg)

	// Test GetConfig
	config := r.GetConfig()
	require.NotNil(t, config)
	assert.Equal(t, 15.0, config.SwitchThreshold)

	_ = s // Suppress unused variable warning
}
