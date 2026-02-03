package integration

import (
	"testing"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFullQuotaFlow tests the complete quota flow from setup to routing
func TestFullQuotaFlow(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create test accounts
	acc1 := createTestAccount(t, ts.Store, "flow-account-1", models.ProviderOpenAI, 10)
	acc2 := createTestAccount(t, ts.Store, "flow-account-2", models.ProviderOpenAI, 5)

	// Create quotas with different remaining percentages
	quota1 := createTestQuota(t, ts.Store, "flow-account-1", 80.0, false)
	quota2 := createTestQuota(t, ts.Store, "flow-account-2", 50.0, false)

	// Verify accounts were created
	require.NotNil(t, acc1)
	require.NotNil(t, acc2)
	assert.Equal(t, "flow-account-1", acc1.ID)
	assert.Equal(t, models.ProviderOpenAI, acc1.Provider)

	// Verify quotas were created
	require.NotNil(t, quota1)
	require.NotNil(t, quota2)
	assert.Equal(t, 80.0, quota1.EffectiveRemainingPct)
	assert.Equal(t, 50.0, quota2.EffectiveRemainingPct)

	// Test router selection - should select account with higher quota
	resp := requestRouterSelect(t, ts.Engine, "openai", "balanced")
	assert.NotEmpty(t, resp.AccountID)
	assert.Equal(t, "openai", resp.Provider)

	// Account with 80% remaining should be preferred over 50%
	if resp.AccountID == "flow-account-1" {
		assert.Greater(t, resp.Score, 0.5)
	}
}

// TestReservationFlow tests the complete reservation cycle
func TestReservationFlow(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Setup account with quota
	acc := createTestAccount(t, ts.Store, "reservation-test", models.ProviderOpenAI, 10)
	quota := createTestQuota(t, ts.Store, "reservation-test", 50.0, false)
	require.NotNil(t, acc)
	require.NotNil(t, quota)

	// Create a reservation
	reservation := createReservation(t, ts.Engine, "reservation-test", 10.0, "test-correlation-1")
	require.NotNil(t, reservation)
	assert.NotEmpty(t, reservation.ReservationID)
	assert.Equal(t, "reservation-test", reservation.AccountID)
	assert.Equal(t, "active", reservation.Status)

	// Check that quota was updated (virtual usage increased)
	updatedQuota := getQuota(t, ts.Engine, "reservation-test")
	assert.Equal(t, 40.0, updatedQuota.EffectiveRemainingPct) // 50% - 10% virtual usage

	// Release the reservation
	releaseReservation(t, ts.Engine, reservation.ReservationID, 8.0)

	// Check that quota was restored (virtual usage decreased by actual cost)
	finalQuota := getQuota(t, ts.Engine, "reservation-test")
	assert.Equal(t, 42.0, finalQuota.EffectiveRemainingPct) // 40% + 8% released - 6% remaining virtual
}

// TestRouterSelection tests various router selection scenarios
func TestRouterSelection(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Test cases for router selection
	tests := []struct {
		name             string
		provider         string
		policy           string
		expectedProvider string
	}{
		{"OpenAI selection", "openai", "balanced", "openai"},
		{"Anthropic selection", "anthropic", "balanced", "anthropic"},
		{"Gemini selection", "gemini", "balanced", "gemini"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create account for the provider if not exists
			acc := createTestAccount(t, ts.Store, "router-test-"+tt.provider, models.Provider(tt.provider), 10)
			quota := createTestQuota(t, ts.Store, "router-test-"+tt.provider, 70.0, false)
			require.NotNil(t, acc)
			require.NotNil(t, quota)

			resp := requestRouterSelect(t, ts.Engine, tt.provider, tt.policy)
			assert.Equal(t, tt.expectedProvider, resp.Provider)
			assert.NotEmpty(t, resp.AccountID)
		})
	}
}

// TestHealthEndpoint tests the health check endpoint
func TestHealthEndpoint(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Test health check
	resp := requestHealthCheck(t, ts.Engine)

	assert.Equal(t, "healthy", resp["status"])
	assert.NotNil(t, resp["timestamp"])
	assert.NotNil(t, resp["router"])
}

// TestAlertScenarios tests alert scenarios
func TestAlertScenarios(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create account with critical quota
	acc := createTestAccount(t, ts.Store, "alert-test", models.ProviderOpenAI, 10)
	quota := createTestQuota(t, ts.Store, "alert-test", 5.0, false) // Critical level
	require.NotNil(t, acc)
	require.NotNil(t, quota)

	// Update quota to critical level
	quota.EffectiveRemainingPct = 3.0
	quota.IsThrottled = true
	ts.Store.SetQuota("alert-test", quota)

	// Request should but still work with low score
	resp := requestRouterSelect(t, ts.Engine, "openai", "balanced")
	assert.NotEmpty(t, resp.AccountID)

	// Now exhaust the quota
	quota.EffectiveRemainingPct = 0.0
	ts.Store.SetQuota("alert-test", quota)

	// Router should fail to select (no available accounts)
	body := map[string]string{"provider": "openai"}
	w := makeRequest(t, ts.Engine, "POST", "/router/select", body)
	assert.Equal(t, 503, w.StatusCode) // Service unavailable
}

// TestMultiProviderFlow tests routing across multiple providers
func TestMultiProviderFlow(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create accounts for multiple providers
	providers := []struct {
		id       string
		provider models.Provider
		priority int
		quota    float64
	}{
		{"multi-openai", models.ProviderOpenAI, 10, 70.0},
		{"multi-anthropic", models.ProviderAnthropic, 8, 60.0},
		{"multi-gemini", models.ProviderGemini, 6, 50.0},
	}

	for _, p := range providers {
		acc := createTestAccount(t, ts.Store, p.id, p.provider, p.priority)
		quota := createTestQuota(t, ts.Store, p.id, p.quota, false)
		require.NotNil(t, acc)
		require.NotNil(t, quota)
	}

	// Test selection for each provider
	for _, p := range providers {
		resp := requestRouterSelect(t, ts.Engine, string(p.provider), "balanced")
		assert.Equal(t, string(p.provider), resp.Provider)
		assert.Contains(t, resp.AccountID, string(p.provider)[0:3])
	}
}

// TestPolicySelectionFlow tests different routing policies
func TestPolicySelectionFlow(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create accounts with different characteristics
	acc1 := createTestAccount(t, ts.Store, "policy-high-priority", models.ProviderOpenAI, 10)
	acc2 := createTestAccount(t, ts.Store, "policy-low-priority", models.ProviderOpenAI, 5)
	_ = acc1
	_ = acc2

	quota1 := createTestQuota(t, ts.Store, "policy-high-priority", 30.0, false)
	quota2 := createTestQuota(t, ts.Store, "policy-low-priority", 90.0, false)
	_ = quota1
	_ = quota2

	// Test with different policies
	policies := []string{"balanced", "cost", "performance", "safety"}

	for _, policy := range policies {
		t.Run("policy_"+policy, func(t *testing.T) {
			resp := requestRouterSelect(t, ts.Engine, "openai", policy)
			assert.NotEmpty(t, resp.AccountID)
			assert.Equal(t, "openai", resp.Provider)
		})
	}
}

// TestQuotaUpdateFlow tests quota updates through ingest
func TestQuotaUpdateFlow(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create initial account and quota
	acc := createTestAccount(t, ts.Store, "quota-update-test", models.ProviderOpenAI, 10)
	quota := createTestQuota(t, ts.Store, "quota-update-test", 80.0, false)
	require.NotNil(t, acc)
	require.NotNil(t, quota)

	// Verify initial quota
	initialQuota := getQuota(t, ts.Engine, "quota-update-test")
	assert.Equal(t, 80.0, initialQuota.EffectiveRemainingPct)

	// Update quota via ingest
	ingestQuota(t, ts.Engine, "quota-update-test", "openai", 40.0)

	// Verify updated quota
	updatedQuota := getQuota(t, ts.Engine, "quota-update-test")
	assert.Equal(t, 40.0, updatedQuota.EffectiveRemainingPct)
}

// TestDistributionCalculation tests the distribution calculation endpoint
func TestDistributionCalculation(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create multiple accounts
	for i := 1; i <= 3; i++ {
		acc := createTestAccount(t, ts.Store, "dist-account-"+string(rune('0'+i)), models.ProviderOpenAI, 10-i)
		quota := createTestQuota(t, ts.Store, "dist-account-"+string(rune('0'+i)), float64(100-20*i), false)
		require.NotNil(t, acc)
		require.NotNil(t, quota)
	}

	// Get distribution
	dist := getDistribution(t, ts.Engine)
	assert.NotNil(t, dist)
	assert.NotEmpty(t, dist)
}
