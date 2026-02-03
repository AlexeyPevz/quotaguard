package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/collector"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/reservation"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestServer() (*Server, *store.MemoryStore) {
	gin.SetMode(gin.TestMode)

	s := store.NewMemoryStore()
	cfg := config.ServerConfig{Host: "localhost", HTTPPort: 8080}
	apiCfg := config.APIConfig{
		Auth: config.AuthConfig{
			Enabled: false,
		},
	}
	r := router.NewRouter(s, router.DefaultConfig())
	rm := reservation.NewManager(s, reservation.DefaultConfig())
	c := collector.NewPassiveCollector(s, 100, 0)

	return NewServer(cfg, apiCfg, s, r, rm, c), s
}

func TestHandleHealth(t *testing.T) {
	server, _ := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "healthy")
}

func TestHandleRouterSelect(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	acc := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
		Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 200, Remaining: 800}},
	}
	s.SetAccount(acc)
	s.SetQuota("acc-1", quota)

	body := RouterSelectRequest{
		Provider:      "openai",
		EstimatedCost: 10.0,
		Policy:        "balanced",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp RouterSelectResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "acc-1", resp.AccountID)
	assert.Equal(t, "openai", resp.Provider)
}

func TestHandleRouterSelectNoAccounts(t *testing.T) {
	server, _ := setupTestServer()

	body := RouterSelectRequest{
		Provider:      "openai",
		EstimatedCost: 10.0,
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/router/select", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleRouterFeedback(t *testing.T) {
	server, _ := setupTestServer()

	body := RouterFeedbackRequest{
		AccountID:  "acc-1",
		ActualCost: 5.0,
		Success:    true,
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleListQuotas(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
	}
	s.SetQuota("acc-1", quota)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/quotas", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "acc-1")
}

func TestHandleGetQuota(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
	}
	s.SetQuota("acc-1", quota)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/quotas/acc-1", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.QuotaInfo
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "acc-1", resp.AccountID)
}

func TestHandleGetQuotaNotFound(t *testing.T) {
	server, _ := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/quotas/non-existent", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleCreateReservation(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	acc := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 80.0,
	}
	s.SetAccount(acc)
	s.SetQuota("acc-1", quota)

	body := CreateReservationRequest{
		AccountID:        "acc-1",
		EstimatedCostPct: 10.0,
		CorrelationID:    "test-correlation",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp CreateReservationResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ReservationID)
	assert.Equal(t, "acc-1", resp.AccountID)
}

func TestHandleCreateReservationInvalid(t *testing.T) {
	server, _ := setupTestServer()

	body := CreateReservationRequest{
		AccountID:        "acc-1",
		EstimatedCostPct: 150.0, // Invalid: > 100
		CorrelationID:    "test-correlation",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetReservation(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	acc := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 80.0,
	}
	s.SetAccount(acc)
	s.SetQuota("acc-1", quota)

	// Create reservation
	res, _ := server.reservation.Create(context.Background(), "acc-1", 10.0, "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/reservations/"+res.ID, nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.Reservation
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, res.ID, resp.ID)
}

func TestHandleGetReservationNotFound(t *testing.T) {
	server, _ := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/reservations/non-existent", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleCancelReservation(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	acc := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 80.0,
	}
	s.SetAccount(acc)
	s.SetQuota("acc-1", quota)

	// Create reservation
	res, _ := server.reservation.Create(context.Background(), "acc-1", 10.0, "test")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/reservations/"+res.ID+"/cancel", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleReleaseReservation(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	acc := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 80.0,
	}
	s.SetAccount(acc)
	s.SetQuota("acc-1", quota)

	// Create reservation
	res, _ := server.reservation.Create(context.Background(), "acc-1", 10.0, "test")

	body := map[string]float64{"actual_cost_percent": 5.0}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/reservations/"+res.ID+"/release", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleIngest(t *testing.T) {
	server, _ := setupTestServer()

	body := IngestRequest{
		AccountID:             "acc-1",
		Provider:              "openai",
		EffectiveRemainingPct: 75.0,
		Source:                "test",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ingested")
}

func TestHandleIngestInvalid(t *testing.T) {
	server, _ := setupTestServer()

	body := map[string]string{
		"account_id": "acc-1",
		// Missing required "provider" field
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRouterDistribution(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	acc1 := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	acc2 := &models.Account{ID: "acc-2", Provider: models.ProviderOpenAI, Enabled: true}
	s.SetAccount(acc1)
	s.SetAccount(acc2)

	quota1 := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 80.0,
		Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 200, Remaining: 800}},
	}
	quota2 := &models.QuotaInfo{
		AccountID:             "acc-2",
		EffectiveRemainingPct: 60.0,
		Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 400, Remaining: 600}},
	}
	s.SetQuota("acc-1", quota1)
	s.SetQuota("acc-2", quota2)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/router/distribution", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Check that response contains distribution data
	var distribution map[string]float64
	err := json.Unmarshal(w.Body.Bytes(), &distribution)
	require.NoError(t, err)
	assert.Len(t, distribution, 2)
	assert.Contains(t, distribution, "acc-1")
	assert.Contains(t, distribution, "acc-2")
}

func TestHandleReleaseReservationNotFound(t *testing.T) {
	server, _ := setupTestServer()

	body := map[string]float64{"actual_cost_percent": 5.0}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/reservations/non-existent/release", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCancelReservationNotFound(t *testing.T) {
	server, _ := setupTestServer()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/reservations/non-existent/cancel", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleCreateReservationInsufficientQuota(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data with low quota
	acc := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 5.0, // Very low quota
	}
	s.SetAccount(acc)
	s.SetQuota("acc-1", quota)

	body := CreateReservationRequest{
		AccountID:        "acc-1",
		EstimatedCostPct: 10.0, // More than available
		CorrelationID:    "test-correlation",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/reservations", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleRouterFeedbackWithReservation(t *testing.T) {
	server, s := setupTestServer()

	// Setup test data
	acc := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 80.0,
	}
	s.SetAccount(acc)
	s.SetQuota("acc-1", quota)

	// Create reservation
	res, _ := server.reservation.Create(context.Background(), "acc-1", 10.0, "test")

	body := RouterFeedbackRequest{
		AccountID:     "acc-1",
		ReservationID: res.ID,
		ActualCost:    5.0,
		Success:       true,
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/router/feedback", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleIngestWithCollector(t *testing.T) {
	server, _ := setupTestServer()

	body := IngestRequest{
		AccountID:             "acc-1",
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
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ingested")
}
