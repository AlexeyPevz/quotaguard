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

// setupAPITestServer creates a test server for API endpoint testing
func setupAPITestServer(t *testing.T) (*gin.Engine, *store.SQLiteStore, func()) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "api_test.db")

	s, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err, "Failed to create SQLite store")

	cfg := config.ServerConfig{Host: "localhost", HTTPPort: 8080}
	apiCfg := config.APIConfig{
		Auth: config.AuthConfig{
			Enabled: false,
		},
	}
	rtr := router.NewRouter(s, router.DefaultConfig())
	rm := reservation.NewManager(s, reservation.DefaultConfig())
	pc := collector.NewPassiveCollector(s, 100, 100*time.Millisecond)

	srv := api.NewServer(cfg, apiCfg, s, rtr, rm, pc)

	ctx := context.Background()
	err = pc.Start(ctx)
	require.NoError(t, err, "Failed to start passive collector")

	cleanup := func() {
		require.NoError(t, pc.Stop())
		_ = s.Close()
	}

	return srv.Router(), s, cleanup
}

func TestAPI_RouterSelect(t *testing.T) {
	router, s, cleanup := setupAPITestServer(t)
	defer cleanup()

	// Setup test data
	acc := &models.Account{
		ID:         "api-test-acc",
		Provider:   models.ProviderOpenAI,
		Tier:       "tier-1",
		Enabled:    true,
		Priority:   10,
		InputCost:  0.01,
		OutputCost: 0.03,
	}
	s.SetAccount(acc)

	quota := &models.QuotaInfo{
		AccountID:             "api-test-acc",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
		Confidence:            0.95,
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 3500, Used: 700, Remaining: 2800},
		},
	}
	s.SetQuota("api-test-acc", quota)

	t.Run("successful selection", func(t *testing.T) {
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
		assert.Equal(t, "api-test-acc", resp.AccountID)
		assert.Equal(t, "openai", resp.Provider)
		assert.Greater(t, resp.Score, 0.0)
	})

	t.Run("with exclude list", func(t *testing.T) {
		body := api.RouterSelectRequest{
			Provider:      "openai",
			EstimatedCost: 5.0,
			Policy:        "balanced",
			Exclude:       []string{"api-test-acc"},
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		// Should return 503 as no accounts are available
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("with required dimensions", func(t *testing.T) {
		body := api.RouterSelectRequest{
			Provider:      "openai",
			RequiredDims:  []string{"RPM"},
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
		assert.Equal(t, "api-test-acc", resp.AccountID)
	})

	t.Run("invalid request body", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestAPI_RouterFeedback(t *testing.T) {
	router, s, cleanup := setupAPITestServer(t)
	defer cleanup()

	// Setup test account
	acc := &models.Account{
		ID:       "feedback-acc",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	t.Run("successful feedback", func(t *testing.T) {
		body := api.RouterFeedbackRequest{
			AccountID:  "feedback-acc",
			ActualCost: 5.5,
			Success:    true,
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "feedback recorded")
	})

	t.Run("feedback with error", func(t *testing.T) {
		body := api.RouterFeedbackRequest{
			AccountID:  "feedback-acc",
			ActualCost: 0,
			Success:    false,
			Error:      "rate_limit_exceeded",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("missing account_id", func(t *testing.T) {
		body := api.RouterFeedbackRequest{
			ActualCost: 5.5,
			Success:    true,
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestAPI_IngestHeaders(t *testing.T) {
	router, s, cleanup := setupAPITestServer(t)
	defer cleanup()

	// Setup test account
	acc := &models.Account{
		ID:       "ingest-acc",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	t.Run("successful ingest", func(t *testing.T) {
		body := api.IngestRequest{
			AccountID:             "ingest-acc",
			Provider:              "openai",
			EffectiveRemainingPct: 75.0,
			Source:                "headers",
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
	})

	t.Run("missing account_id", func(t *testing.T) {
		body := api.IngestRequest{
			Provider:              "openai",
			EffectiveRemainingPct: 75.0,
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing provider", func(t *testing.T) {
		body := api.IngestRequest{
			AccountID:             "ingest-acc",
			EffectiveRemainingPct: 75.0,
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("with throttle status", func(t *testing.T) {
		body := api.IngestRequest{
			AccountID:             "ingest-acc",
			Provider:              "openai",
			EffectiveRemainingPct: 5.0,
			IsThrottled:           true,
			Source:                "headers",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestAPI_GetQuotas(t *testing.T) {
	router, s, cleanup := setupAPITestServer(t)
	defer cleanup()

	// Setup test data - create accounts first
	accounts := []*models.Account{
		{
			ID:       "quota-acc-1",
			Provider: models.ProviderOpenAI,
			Enabled:  true,
			Priority: 10,
		},
		{
			ID:       "quota-acc-2",
			Provider: models.ProviderAnthropic,
			Enabled:  true,
			Priority: 8,
		},
	}
	for _, acc := range accounts {
		s.SetAccount(acc)
	}

	// Setup quotas
	quotas := []*models.QuotaInfo{
		{
			AccountID:             "quota-acc-1",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 80.0,
		},
		{
			AccountID:             "quota-acc-2",
			Provider:              models.ProviderAnthropic,
			EffectiveRemainingPct: 65.0,
		},
	}

	for _, q := range quotas {
		s.SetQuota(q.AccountID, q)
	}

	t.Run("list all quotas", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/quotas", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp []models.QuotaInfo
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp, 2)
	})

	t.Run("get specific quota", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/quotas/quota-acc-1", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp models.QuotaInfo
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "quota-acc-1", resp.AccountID)
		assert.Equal(t, 80.0, resp.EffectiveRemainingPct)
	})

	t.Run("quota not found", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/quotas/non-existent", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestAPI_Health(t *testing.T) {
	router, _, cleanup := setupAPITestServer(t)
	defer cleanup()

	t.Run("health check healthy", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/health", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "healthy", resp["status"])
		assert.Contains(t, resp, "router")
	})

	t.Run("health check with router status", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/health", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		// Router field should be present
		assert.Contains(t, resp, "router")
	})
}

func TestAPI_Metrics(t *testing.T) {
	router, _, cleanup := setupAPITestServer(t)
	defer cleanup()

	t.Run("metrics endpoint", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/metrics", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		// Prometheus format should contain various metrics
		assert.NotEmpty(t, body)
	})
}

func TestAPI_RouterDistribution(t *testing.T) {
	router, s, cleanup := setupAPITestServer(t)
	defer cleanup()

	// Setup test data
	acc1 := &models.Account{
		ID:       "dist-acc-1",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	acc2 := &models.Account{
		ID:       "dist-acc-2",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 5,
	}
	s.SetAccount(acc1)
	s.SetAccount(acc2)

	quota1 := &models.QuotaInfo{
		AccountID:             "dist-acc-1",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 90.0,
		Confidence:            0.95,
	}
	quota2 := &models.QuotaInfo{
		AccountID:             "dist-acc-2",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 50.0,
		Confidence:            0.85,
	}
	s.SetQuota("dist-acc-1", quota1)
	s.SetQuota("dist-acc-2", quota2)

	t.Run("get distribution", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/router/distribution", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var distribution map[string]float64
		err := json.Unmarshal(w.Body.Bytes(), &distribution)
		require.NoError(t, err)
		assert.Len(t, distribution, 2)
		// First account should have higher distribution due to better quota
		assert.GreaterOrEqual(t, distribution["dist-acc-1"], distribution["dist-acc-2"])
	})
}

func TestAPI_Reservations(t *testing.T) {
	router, s, cleanup := setupAPITestServer(t)
	defer cleanup()

	// Setup account with quota
	acc := &models.Account{
		ID:       "res-acc",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	quota := &models.QuotaInfo{
		AccountID:             "res-acc",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
	}
	s.SetQuota("res-acc", quota)

	t.Run("create reservation", func(t *testing.T) {
		body := api.CreateReservationRequest{
			AccountID:        "res-acc",
			EstimatedCostPct: 10.0,
			CorrelationID:    "test-correlation",
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
		assert.Equal(t, "res-acc", resp.AccountID)
		assert.Equal(t, string(models.ReservationActive), resp.Status)
	})

	t.Run("create reservation invalid cost", func(t *testing.T) {
		body := api.CreateReservationRequest{
			AccountID:        "res-acc",
			EstimatedCostPct: 150.0, // Invalid: > 100
			CorrelationID:    "test-correlation",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("create reservation insufficient quota", func(t *testing.T) {
		// Set low quota
		lowQuota := &models.QuotaInfo{
			AccountID:             "res-acc",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 5.0, // Very low
		}
		s.SetQuota("res-acc", lowQuota)

		body := api.CreateReservationRequest{
			AccountID:        "res-acc",
			EstimatedCostPct: 10.0, // More than available
			CorrelationID:    "test-correlation",
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("release reservation", func(t *testing.T) {
		s.SetQuota("res-acc", &models.QuotaInfo{
			AccountID:             "res-acc",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 80.0,
		})

		// Create reservation first
		body := api.CreateReservationRequest{
			AccountID:        "res-acc",
			EstimatedCostPct: 10.0,
			CorrelationID:    "release-test",
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
		releaseBody := map[string]float64{"actual_cost_percent": 8.0}
		releaseJsonBody, _ := json.Marshal(releaseBody)

		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/reservations/"+reservationID+"/release", bytes.NewBuffer(releaseJsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("cancel reservation", func(t *testing.T) {
		s.SetQuota("res-acc", &models.QuotaInfo{
			AccountID:             "res-acc",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 80.0,
		})

		// Create reservation first
		body := api.CreateReservationRequest{
			AccountID:        "res-acc",
			EstimatedCostPct: 10.0,
			CorrelationID:    "cancel-test",
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

	t.Run("get reservation not found", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/reservations/non-existent", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
