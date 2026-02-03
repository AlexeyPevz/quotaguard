package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/api"
	"github.com/quotaguard/quotaguard/internal/collector"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/reservation"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestServer creates a test server with SQLite database
func setupTestServer(t *testing.T) (*gin.Engine, *store.SQLiteStore, func()) {
	gin.SetMode(gin.TestMode)

	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err, "Failed to create SQLite store")

	// Setup components
	cfg := config.ServerConfig{Host: "localhost", HTTPPort: 8080}
	apiCfg := config.APIConfig{
		Auth: config.AuthConfig{
			Enabled: false,
		},
	}
	rtr := router.NewRouter(s, router.DefaultConfig())
	rm := reservation.NewManager(s, reservation.DefaultConfig())
	pc := collector.NewPassiveCollector(s, 100, 100*time.Millisecond)

	// Create server
	srv := api.NewServer(cfg, apiCfg, s, rtr, rm, pc)

	// Start passive collector
	ctx := context.Background()
	err = pc.Start(ctx)
	require.NoError(t, err, "Failed to start passive collector")

	cleanup := func() {
		require.NoError(t, pc.Stop())
		_ = s.Close()
	}

	return srv.Router(), s, cleanup
}

// setupTestAccounts creates a set of test accounts with quotas
func setupTestAccounts(t *testing.T, s *store.SQLiteStore) {
	accounts := []*models.Account{
		{
			ID:               "openai-primary",
			Provider:         models.ProviderOpenAI,
			Tier:             "tier-1",
			Enabled:          true,
			Priority:         10,
			ConcurrencyLimit: 100,
			InputCost:        0.01,
			OutputCost:       0.03,
		},
		{
			ID:               "openai-secondary",
			Provider:         models.ProviderOpenAI,
			Tier:             "tier-2",
			Enabled:          true,
			Priority:         5,
			ConcurrencyLimit: 50,
			InputCost:        0.015,
			OutputCost:       0.045,
		},
		{
			ID:               "anthropic-primary",
			Provider:         models.ProviderAnthropic,
			Tier:             "tier-1",
			Enabled:          true,
			Priority:         8,
			ConcurrencyLimit: 80,
			InputCost:        0.015,
			OutputCost:       0.075,
		},
	}

	for _, acc := range accounts {
		s.SetAccount(acc)
	}

	// Setup quotas
	quotas := []*models.QuotaInfo{
		{
			AccountID:             "openai-primary",
			Provider:              models.ProviderOpenAI,
			Tier:                  "tier-1",
			EffectiveRemainingPct: 80.0,
			Confidence:            0.95,
			Source:                models.SourceHeaders,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 3500, Used: 700, Remaining: 2800, RefillRate: 0.5},
				{Type: models.DimensionTPM, Limit: 100000, Used: 20000, Remaining: 80000, RefillRate: 0.3},
			},
		},
		{
			AccountID:             "openai-secondary",
			Provider:              models.ProviderOpenAI,
			Tier:                  "tier-2",
			EffectiveRemainingPct: 50.0,
			Confidence:            0.85,
			Source:                models.SourceHeaders,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 3500, Used: 1750, Remaining: 1750, RefillRate: 0.4},
				{Type: models.DimensionTPM, Limit: 100000, Used: 50000, Remaining: 50000, RefillRate: 0.2},
			},
		},
		{
			AccountID:             "anthropic-primary",
			Provider:              models.ProviderAnthropic,
			Tier:                  "tier-1",
			EffectiveRemainingPct: 90.0,
			Confidence:            0.98,
			Source:                models.SourceHeaders,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 1000, Used: 100, Remaining: 900, RefillRate: 0.6},
				{Type: models.DimensionTPM, Limit: 100000, Used: 10000, Remaining: 90000, RefillRate: 0.4},
			},
		},
	}

	for _, q := range quotas {
		s.SetQuota(q.AccountID, q)
	}
}

func TestFullRouterSelectionFlow(t *testing.T) {
	router, s, cleanup := setupTestServer(t)
	defer cleanup()

	setupTestAccounts(t, s)

	// Test router selection flow
	t.Run("select best OpenAI account", func(t *testing.T) {
		body := api.RouterSelectRequest{
			Provider:      "openai",
			EstimatedCost: 5.0,
			Policy:        "balanced",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp api.RouterSelectResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.AccountID)
		assert.Equal(t, "openai", resp.Provider)
		assert.Greater(t, resp.Score, 0.0)
		assert.NotEmpty(t, resp.Reason)
	})

	t.Run("select best Anthropic account", func(t *testing.T) {
		body := api.RouterSelectRequest{
			Provider:      "anthropic",
			EstimatedCost: 10.0,
			Policy:        "balanced",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp api.RouterSelectResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "anthropic-primary", resp.AccountID)
		assert.Equal(t, "anthropic", resp.Provider)
	})

	t.Run("router with exclude list", func(t *testing.T) {
		body := api.RouterSelectRequest{
			Provider:      "openai",
			EstimatedCost: 5.0,
			Policy:        "balanced",
			Exclude:       []string{"openai-primary"},
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp api.RouterSelectResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "openai-secondary", resp.AccountID)
	})
}

func TestQuotaIngestionAndStorage(t *testing.T) {
	router, s, cleanup := setupTestServer(t)
	defer cleanup()

	// Create test account
	acc := &models.Account{
		ID:       "test-account",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	t.Run("ingest quota via API", func(t *testing.T) {
		body := api.IngestRequest{
			AccountID:             "test-account",
			Provider:              "openai",
			EffectiveRemainingPct: 75.0,
			Source:                "test",
			Dimensions: []models.Dimension{
				{Type: models.DimensionRPM, Limit: 1000, Used: 250, Remaining: 750},
			},
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify quota was stored
		time.Sleep(150 * time.Millisecond) // Wait for buffer flush
		quota, ok := s.GetQuota("test-account")
		require.True(t, ok)
		assert.Equal(t, "test-account", quota.AccountID)
		assert.Equal(t, 75.0, quota.EffectiveRemainingPct)
	})

	t.Run("list all quotas", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/quotas", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test-account")
	})

	t.Run("get specific quota", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/quotas/test-account", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp models.QuotaInfo
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "test-account", resp.AccountID)
	})
}

func TestReservationLifecycle(t *testing.T) {
	router, s, cleanup := setupTestServer(t)
	defer cleanup()

	// Setup account with quota
	acc := &models.Account{
		ID:       "reservation-test",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	quota := &models.QuotaInfo{
		AccountID:             "reservation-test",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 1000, Used: 200, Remaining: 800},
		},
	}
	s.SetQuota("reservation-test", quota)

	t.Run("create reservation", func(t *testing.T) {
		body := api.CreateReservationRequest{
			AccountID:        "reservation-test",
			EstimatedCostPct: 10.0,
			CorrelationID:    "test-correlation-1",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp api.CreateReservationResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ReservationID)
		assert.Equal(t, "reservation-test", resp.AccountID)
		assert.Equal(t, string(models.ReservationActive), resp.Status)
	})

	t.Run("get reservation", func(t *testing.T) {
		// Create reservation first
		body := api.CreateReservationRequest{
			AccountID:        "reservation-test",
			EstimatedCostPct: 5.0,
			CorrelationID:    "test-correlation-2",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var createResp api.CreateReservationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
		reservationID := createResp.ReservationID

		// Get reservation
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/reservations/"+reservationID, nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp models.Reservation
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, reservationID, resp.ID)
		assert.Equal(t, "reservation-test", resp.AccountID)
		assert.Equal(t, models.ReservationActive, resp.Status)
	})

	t.Run("release reservation", func(t *testing.T) {
		// Create reservation first
		body := api.CreateReservationRequest{
			AccountID:        "reservation-test",
			EstimatedCostPct: 8.0,
			CorrelationID:    "test-correlation-3",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var createResp api.CreateReservationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
		reservationID := createResp.ReservationID

		// Release reservation
		releaseBody := map[string]float64{"actual_cost_percent": 6.0}
		releaseJsonBody, _ := json.Marshal(releaseBody)

		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/reservations/"+reservationID+"/release", bytes.NewBuffer(releaseJsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("cancel reservation", func(t *testing.T) {
		// Create reservation first
		body := api.CreateReservationRequest{
			AccountID:        "reservation-test",
			EstimatedCostPct: 7.0,
			CorrelationID:    "test-correlation-4",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var createResp api.CreateReservationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
		reservationID := createResp.ReservationID

		// Cancel reservation
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/reservations/"+reservationID+"/cancel", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHealthCheckFlow(t *testing.T) {
	router, _, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("health check returns healthy", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/health", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "healthy")
	})

	t.Run("health check with router status", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/health", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Contains(t, resp, "status")
		assert.Contains(t, resp, "router")
	})
}

func TestRouterFailoverScenarios(t *testing.T) {
	router, s, cleanup := setupTestServer(t)
	defer cleanup()

	// Setup accounts for failover testing
	acc1 := &models.Account{
		ID:       "failover-primary",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	acc2 := &models.Account{
		ID:       "failover-secondary",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 5,
	}
	s.SetAccount(acc1)
	s.SetAccount(acc2)

	// Setup quotas - primary has low quota
	quotaPrimary := &models.QuotaInfo{
		AccountID:             "failover-primary",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 3.0, // Below critical threshold
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 100, Used: 97, Remaining: 3},
		},
	}
	quotaSecondary := &models.QuotaInfo{
		AccountID:             "failover-secondary",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 70.0,
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 1000, Used: 300, Remaining: 700},
		},
	}
	s.SetQuota("failover-primary", quotaPrimary)
	s.SetQuota("failover-secondary", quotaSecondary)

	t.Run("router selects secondary when primary is critical", func(t *testing.T) {
		body := api.RouterSelectRequest{
			Provider:      "openai",
			EstimatedCost: 5.0,
			Policy:        "balanced",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp api.RouterSelectResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		// Should select secondary due to primary being critical
		assert.Equal(t, "failover-secondary", resp.AccountID)
	})

	t.Run("router falls back when all accounts are critical", func(t *testing.T) {
		// Set secondary to critical too
		quotaSecondaryCritical := &models.QuotaInfo{
			AccountID:             "failover-secondary",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 2.0, // Below critical threshold
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 100, Used: 98, Remaining: 2},
			},
		}
		s.SetQuota("failover-secondary", quotaSecondaryCritical)

		body := api.RouterSelectRequest{
			Provider:      "openai",
			EstimatedCost: 5.0,
			Policy:        "balanced",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		// Should still return a result but with low score
		assert.Equal(t, http.StatusOK, w.Code)

		var resp api.RouterSelectResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.AccountID)
	})

	t.Run("router distribution endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/router/distribution", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var distribution map[string]float64
		err := json.Unmarshal(w.Body.Bytes(), &distribution)
		require.NoError(t, err)
		assert.Len(t, distribution, 2)
		assert.Contains(t, distribution, "failover-primary")
		assert.Contains(t, distribution, "failover-secondary")
	})
}

func TestRouterFeedbackLoop(t *testing.T) {
	router, s, cleanup := setupTestServer(t)
	defer cleanup()

	// Setup account
	acc := &models.Account{
		ID:       "feedback-test",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	quota := &models.QuotaInfo{
		AccountID:             "feedback-test",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 75.0,
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 1000, Used: 250, Remaining: 750},
		},
	}
	s.SetQuota("feedback-test", quota)

	t.Run("send feedback for account", func(t *testing.T) {
		body := api.RouterFeedbackRequest{
			AccountID:  "feedback-test",
			ActualCost: 8.5,
			Success:    true,
			Error:      "",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("send feedback with reservation", func(t *testing.T) {
		// Create reservation first
		body := api.CreateReservationRequest{
			AccountID:        "feedback-test",
			EstimatedCostPct: 10.0,
			CorrelationID:    "feedback-correlation",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var createResp api.CreateReservationResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
		reservationID := createResp.ReservationID

		// Send feedback with reservation
		feedbackBody := api.RouterFeedbackRequest{
			AccountID:     "feedback-test",
			ReservationID: reservationID,
			ActualCost:    9.5,
			Success:       true,
		}
		feedbackJsonBody, _ := json.Marshal(feedbackBody)

		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(feedbackJsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("send negative feedback", func(t *testing.T) {
		body := api.RouterFeedbackRequest{
			AccountID:  "feedback-test",
			ActualCost: 12.0,
			Success:    false,
			Error:      "rate_limit",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestCollectorBufferFlush(t *testing.T) {
	router, s, cleanup := setupTestServer(t)
	defer cleanup()

	// Create account
	acc := &models.Account{
		ID:       "buffer-test",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	t.Run("buffer flush on size limit", func(t *testing.T) {
		// The buffer size is 100, so we need to ingest 100 items
		for i := 0; i < 100; i++ {
			body := api.IngestRequest{
				AccountID:             "buffer-test",
				Provider:              "openai",
				EffectiveRemainingPct: 80.0,
				Source:                "test",
			}
			jsonBody, _ := json.Marshal(body)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			if i < 99 {
				assert.Equal(t, http.StatusOK, w.Code)
			}
		}

		// The 100th item should trigger flush
		time.Sleep(200 * time.Millisecond)

		// Verify quota was stored
		quota, ok := s.GetQuota("buffer-test")
		require.True(t, ok)
		assert.Equal(t, "buffer-test", quota.AccountID)
	})
}

func TestMetricsEndpoint(t *testing.T) {
	router, _, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("metrics endpoint returns data", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/metrics", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.NotEmpty(t, body)
	})
}

func TestAccountManagement(t *testing.T) {
	router, s, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("get account status", func(t *testing.T) {
		acc := &models.Account{
			ID:       "status-test",
			Provider: models.ProviderAnthropic,
			Enabled:  true,
			Priority: 8,
		}
		s.SetAccount(acc)

		quota := &models.QuotaInfo{
			AccountID:             "status-test",
			Provider:              models.ProviderAnthropic,
			EffectiveRemainingPct: 65.0,
		}
		s.SetQuota("status-test", quota)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/quotas/status-test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp models.QuotaInfo
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "status-test", resp.AccountID)
		assert.Equal(t, 65.0, resp.EffectiveRemainingPct)
	})
}
