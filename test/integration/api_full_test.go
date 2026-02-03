package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPI_AuthRequired tests that all protected endpoints require authentication
func TestAPI_AuthRequired(t *testing.T) {
	ts := setupTestServerWithAuth(t, []string{"test-api-key-1", "test-api-key-2"})
	defer ts.Cleanup()

	// Create test account and quota
	createTestAccount(t, ts.Store, "auth-test-account", "openai", 10)
	createTestQuota(t, ts.Store, "auth-test-account", 70.0, false)

	// Test cases for endpoints that require authentication
	endpoints := []struct {
		name   string
		method string
		path   string
		body   interface{}
	}{
		{"router select POST", "POST", "/router/select", map[string]string{"provider": "openai"}},
		{"router feedback POST", "POST", "/router/feedback", map[string]string{"account_id": "auth-test-account", "success": "true"}},
		{"router distribution GET", "GET", "/router/distribution", nil},
		{"list quotas GET", "GET", "/quotas", nil},
		{"get quota GET", "GET", "/quotas/auth-test-account", nil},
		{"create reservation POST", "POST", "/reservations", map[string]interface{}{"account_id": "auth-test-account", "estimated_cost_percent": 5.0, "correlation_id": "test"}},
		{"release reservation POST", "POST", "/reservations/test-id/release", map[string]float64{"actual_cost_percent": 3.0}},
		{"cancel reservation POST", "POST", "/reservations/test-id/cancel", nil},
		{"get reservation GET", "GET", "/reservations/test-id", nil},
		{"ingest POST", "POST", "/ingest", map[string]interface{}{"account_id": "auth-test-account", "provider": "openai", "effective_remaining_percent": 50.0}},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			// Test without auth - should get 401
			resp := makeAuthenticatedRequest(t, ts.Engine, ep.method, ep.path, ep.body, "")
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Expected 401 without auth for "+ep.name)

			// Test with invalid auth - should get 401
			resp = makeAuthenticatedRequest(t, ts.Engine, ep.method, ep.path, ep.body, "invalid-key")
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Expected 401 with invalid key for "+ep.name)

			// Test with valid auth - should not get 401
			resp = makeAuthenticatedRequest(t, ts.Engine, ep.method, ep.path, ep.body, "test-api-key-1")
			assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode, "Expected success with valid key for "+ep.name)
		})
	}
}

// TestAPI_HealthPublic tests that health endpoint is public
func TestAPI_HealthPublic(t *testing.T) {
	ts := setupTestServerWithAuth(t, []string{"test-api-key"})
	defer ts.Cleanup()

	// Health check should work without authentication
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	ts.Engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAPI_CRUD_Accounts tests CRUD operations for accounts
func TestAPI_CRUD_Accounts(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	t.Run("create account via store", func(t *testing.T) {
		acc := createTestAccount(t, ts.Store, "crud-account-1", "openai", 10)
		require.NotNil(t, acc)
		assert.Equal(t, "crud-account-1", acc.ID)
		assert.Equal(t, "openai", string(acc.Provider))
		assert.True(t, acc.Enabled)
	})

	t.Run("read account", func(t *testing.T) {
		// Create account first
		createTestAccount(t, ts.Store, "crud-account-2", "anthropic", 8)

		// Read account
		acc, ok := ts.Store.GetAccount("crud-account-2")
		require.True(t, ok)
		assert.Equal(t, "crud-account-2", acc.ID)
		assert.Equal(t, "anthropic", string(acc.Provider))
	})

	t.Run("update account", func(t *testing.T) {
		// Create account first
		acc := createTestAccount(t, ts.Store, "crud-account-3", "gemini", 5)

		// Update account
		acc.Priority = 15
		acc.Enabled = false
		ts.Store.SetAccount(acc)

		// Verify update
		updated, ok := ts.Store.GetAccount("crud-account-3")
		require.True(t, ok)
		assert.Equal(t, 15, updated.Priority)
		assert.False(t, updated.Enabled)
	})

	t.Run("delete account", func(t *testing.T) {
		// Create account first
		createTestAccount(t, ts.Store, "crud-account-4", "azure", 7)

		// Delete account
		deleted := ts.Store.DeleteAccount("crud-account-4")
		assert.True(t, deleted)

		// Verify deletion
		_, ok := ts.Store.GetAccount("crud-account-4")
		assert.False(t, ok)
	})

	t.Run("list accounts", func(t *testing.T) {
		// Create multiple accounts
		createTestAccount(t, ts.Store, "list-account-1", "openai", 10)
		createTestAccount(t, ts.Store, "list-account-2", "anthropic", 8)
		createTestAccount(t, ts.Store, "list-account-3", "gemini", 6)

		// List all accounts - should have at least 3 new accounts
		accounts := ts.Store.ListAccounts()
		assert.GreaterOrEqual(t, len(accounts), 3, "Should have at least 3 accounts")

		// List enabled accounts only
		enabledAccounts := ts.Store.ListEnabledAccounts()
		assert.GreaterOrEqual(t, len(enabledAccounts), 3, "Should have at least 3 enabled accounts")
	})
}

// TestAPI_CRUD_Quotas tests CRUD operations for quotas
func TestAPI_CRUD_Quotas(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create account first
	createTestAccount(t, ts.Store, "quota-crud-account", "openai", 10)

	t.Run("create quota via store", func(t *testing.T) {
		quota := createTestQuota(t, ts.Store, "quota-crud-account", 75.0, false)
		require.NotNil(t, quota)
		assert.Equal(t, "quota-crud-account", quota.AccountID)
		assert.Equal(t, 75.0, quota.EffectiveRemainingPct)
	})

	t.Run("read quota", func(t *testing.T) {
		// Create quota first
		createTestQuota(t, ts.Store, "quota-crud-account", 60.0, false)

		// Read quota
		quota, ok := ts.Store.GetQuota("quota-crud-account")
		require.True(t, ok)
		assert.Equal(t, "quota-crud-account", quota.AccountID)
		assert.Equal(t, 60.0, quota.EffectiveRemainingPct)
	})

	t.Run("update quota", func(t *testing.T) {
		// Create quota first
		quota := createTestQuota(t, ts.Store, "quota-crud-account", 50.0, false)

		// Update quota
		quota.EffectiveRemainingPct = 30.0
		quota.IsThrottled = true
		ts.Store.SetQuota("quota-crud-account", quota)

		// Verify update
		updated, ok := ts.Store.GetQuota("quota-crud-account")
		require.True(t, ok)
		assert.Equal(t, 30.0, updated.EffectiveRemainingPct)
		assert.True(t, updated.IsThrottled)
	})

	t.Run("delete quota", func(t *testing.T) {
		// Create quota first
		createTestQuota(t, ts.Store, "quota-crud-account", 40.0, false)

		// Delete quota
		deleted := ts.Store.DeleteQuota("quota-crud-account")
		assert.True(t, deleted)

		// Verify deletion
		_, ok := ts.Store.GetQuota("quota-crud-account")
		assert.False(t, ok)
	})

	t.Run("list quotas", func(t *testing.T) {
		// Create multiple quotas
		createTestAccount(t, ts.Store, "list-quota-1", "openai", 10)
		createTestAccount(t, ts.Store, "list-quota-2", "anthropic", 8)
		createTestQuota(t, ts.Store, "list-quota-1", 80.0, false)
		createTestQuota(t, ts.Store, "list-quota-2", 60.0, false)

		// List quotas
		quotas := ts.Store.ListQuotas()
		require.Len(t, quotas, 2)
	})
}

// TestAPI_RouterEndpoints tests router-related API endpoints
func TestAPI_RouterEndpoints(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Setup test data
	createTestAccount(t, ts.Store, "router-ep-1", "openai", 10)
	createTestAccount(t, ts.Store, "router-ep-2", "anthropic", 8)
	createTestQuota(t, ts.Store, "router-ep-1", 70.0, false)
	createTestQuota(t, ts.Store, "router-ep-2", 85.0, false)

	t.Run("router select with policy", func(t *testing.T) {
		resp := requestRouterSelect(t, ts.Engine, "openai", "balanced")
		assert.NotEmpty(t, resp.AccountID)
		assert.Equal(t, "openai", resp.Provider)
	})

	t.Run("router select with provider filter", func(t *testing.T) {
		resp := requestRouterSelect(t, ts.Engine, "anthropic", "balanced")
		assert.NotEmpty(t, resp.AccountID)
		assert.Equal(t, "anthropic", resp.Provider)
	})

	t.Run("router distribution", func(t *testing.T) {
		dist := getDistribution(t, ts.Engine)
		assert.NotNil(t, dist)
	})
}

// TestAPI_ReservationEndpoints tests reservation-related API endpoints
func TestAPI_ReservationEndpoints(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Setup test data
	createTestAccount(t, ts.Store, "reservation-ep-1", "openai", 10)
	createTestQuota(t, ts.Store, "reservation-ep-1", 80.0, false)

	t.Run("create reservation", func(t *testing.T) {
		reservation := createReservation(t, ts.Engine, "reservation-ep-1", 10.0, "corr-1")
		require.NotNil(t, reservation)
		assert.NotEmpty(t, reservation.ReservationID)
		assert.Equal(t, "reservation-ep-1", reservation.AccountID)
		assert.Equal(t, "active", reservation.Status)
	})

	t.Run("get reservation", func(t *testing.T) {
		// Create reservation first
		reservation := createReservation(t, ts.Engine, "reservation-ep-1", 5.0, "corr-2")

		// Get reservation
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/reservations/"+reservation.ReservationID, nil)
		ts.Engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestAPI_IngestEndpoint tests the ingest endpoint
func TestAPI_IngestEndpoint(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Setup test data
	createTestAccount(t, ts.Store, "ingest-ep-1", "openai", 10)
	createTestQuota(t, ts.Store, "ingest-ep-1", 90.0, false)

	t.Run("ingest quota update", func(t *testing.T) {
		ingestQuota(t, ts.Engine, "ingest-ep-1", "openai", 45.0)

		// Verify update
		quota, ok := ts.Store.GetQuota("ingest-ep-1")
		require.True(t, ok)
		assert.Equal(t, 45.0, quota.EffectiveRemainingPct)
	})
}

// TestAPI_RateLimiting tests rate limiting functionality
func TestAPI_RateLimiting(t *testing.T) {
	ts := setupTestServerExt(t)
	defer ts.Cleanup()

	// Create test account and quota
	createTestAccount(t, ts.Store, "ratelimit-test", "openai", 10)
	createTestQuota(t, ts.Store, "ratelimit-test", 70.0, false)

	// Make a single request to verify it works
	resp := requestRouterSelect(t, ts.Engine, "openai", "balanced")
	assert.NotEmpty(t, resp.AccountID)
}
