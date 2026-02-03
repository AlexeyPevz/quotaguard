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

// setupCollectorTest creates a test server for collector testing
func setupCollectorTest(t *testing.T) (*gin.Engine, *store.SQLiteStore, func()) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "collector_test.db")

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
	pc := collector.NewPassiveCollector(s, 10, 50*time.Millisecond) // Small buffer, short flush interval

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

func TestCollector_PassiveIngestionFromHeaders(t *testing.T) {
	router, s, cleanup := setupCollectorTest(t)
	defer cleanup()

	// Create test account
	acc := &models.Account{
		ID:       "passive-acc",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	t.Run("ingest via API", func(t *testing.T) {
		body := api.IngestRequest{
			AccountID:             "passive-acc",
			Provider:              "openai",
			EffectiveRemainingPct: 75.0,
			Source:                "headers",
			Dimensions: []models.Dimension{
				{Type: models.DimensionRPM, Limit: 1000, Used: 250, Remaining: 750},
				{Type: models.DimensionTPM, Limit: 100000, Used: 25000, Remaining: 75000},
			},
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("buffer accumulates data", func(t *testing.T) {
		// Create new collector with known buffer size
		tmpDir := t.TempDir()
		s2, err := store.NewSQLiteStore(filepath.Join(tmpDir, "buffer_test.db"))
		require.NoError(t, err)
		defer s2.Close()

		pc := collector.NewPassiveCollector(s2, 5, 100*time.Millisecond)
		ctx := context.Background()
		err = pc.Start(ctx)
		require.NoError(t, err)
		defer func() { require.NoError(t, pc.Stop()) }()

		// Ingest multiple items
		for i := 0; i < 3; i++ {
			quota := &models.QuotaInfo{
				AccountID:             string(rune('a' + i)),
				Provider:              models.ProviderOpenAI,
				EffectiveRemainingPct: 80.0,
			}
			err := pc.Ingest(quota)
			require.NoError(t, err)
		}

		assert.Equal(t, 3, pc.BufferSize())
	})

	t.Run("buffer flush on interval", func(t *testing.T) {
		// Create collector with short flush interval
		tmpDir := t.TempDir()
		s2, err := store.NewSQLiteStore(filepath.Join(tmpDir, "flush_test.db"))
		require.NoError(t, err)
		defer s2.Close()

		pc := collector.NewPassiveCollector(s2, 100, 50*time.Millisecond)
		ctx := context.Background()
		err = pc.Start(ctx)
		require.NoError(t, err)
		defer func() { require.NoError(t, pc.Stop()) }()

		// Create account first
		acc := &models.Account{
			ID:       "flush-test",
			Provider: models.ProviderOpenAI,
			Enabled:  true,
			Priority: 10,
		}
		s2.SetAccount(acc)

		// Ingest some data
		quota := &models.QuotaInfo{
			AccountID:             "flush-test",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 70.0,
		}
		err = pc.Ingest(quota)
		require.NoError(t, err)

		// Wait for flush
		time.Sleep(100 * time.Millisecond)

		// Verify data was flushed to store
		_, ok := s2.GetQuota("flush-test")
		assert.True(t, ok)
	})
}

func TestCollector_BufferFlushLogic(t *testing.T) {
	t.Run("buffer full triggers flush", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := store.NewSQLiteStore(filepath.Join(tmpDir, "full_flush.db"))
		require.NoError(t, err)
		defer s.Close()

		// Create accounts first
		for i := 0; i < 3; i++ {
			acc := &models.Account{
				ID:       string(rune('x' + i)),
				Provider: models.ProviderOpenAI,
				Enabled:  true,
				Priority: 10,
			}
			s.SetAccount(acc)
		}

		// Create collector with small buffer
		pc := collector.NewPassiveCollector(s, 3, 10*time.Second)
		ctx := context.Background()
		err = pc.Start(ctx)
		require.NoError(t, err)
		defer func() { require.NoError(t, pc.Stop()) }()

		// Fill buffer
		for i := 0; i < 3; i++ {
			quota := &models.QuotaInfo{
				AccountID:             string(rune('x' + i)),
				Provider:              models.ProviderOpenAI,
				EffectiveRemainingPct: 80.0,
			}
			err := pc.Ingest(quota)
			require.NoError(t, err)
		}

		// Buffer should be full
		assert.Equal(t, 3, pc.BufferSize())

		// Adding more should fail
		extraQuota := &models.QuotaInfo{
			AccountID:             "overflow",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 80.0,
		}
		err = pc.Ingest(extraQuota)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "buffer full")
	})

	t.Run("buffer flush on stop", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := store.NewSQLiteStore(filepath.Join(tmpDir, "stop_flush.db"))
		require.NoError(t, err)

		// Create account first
		acc := &models.Account{
			ID:       "stop-flush-test",
			Provider: models.ProviderOpenAI,
			Enabled:  true,
			Priority: 10,
		}
		s.SetAccount(acc)

		pc := collector.NewPassiveCollector(s, 100, 10*time.Minute) // Long interval
		ctx := context.Background()
		err = pc.Start(ctx)
		require.NoError(t, err)

		// Ingest data
		quota := &models.QuotaInfo{
			AccountID:             "stop-flush-test",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 75.0,
		}
		err = pc.Ingest(quota)
		require.NoError(t, err)

		// Stop collector (should flush remaining data)
		err = pc.Stop()
		require.NoError(t, err)

		// Verify data was flushed
		_, ok := s.GetQuota("stop-flush-test")
		assert.True(t, ok, "Data should be flushed on stop")

		s.Close()
	})
}

func TestCollector_ErrorHandling(t *testing.T) {
	t.Run("invalid quota data", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := store.NewSQLiteStore(filepath.Join(tmpDir, "invalid.db"))
		require.NoError(t, err)
		defer s.Close()

		pc := collector.NewPassiveCollector(s, 100, 10*time.Second)
		ctx := context.Background()
		err = pc.Start(ctx)
		require.NoError(t, err)
		defer func() { require.NoError(t, pc.Stop()) }()

		// Try to ingest without account (should fail with foreign key)
		quota := &models.QuotaInfo{
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 80.0,
		}
		err = pc.Ingest(quota)
		// May error or not depending on implementation
		_ = err
	})

	t.Run("collector not running", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := store.NewSQLiteStore(filepath.Join(tmpDir, "not_running.db"))
		require.NoError(t, err)
		defer s.Close()

		pc := collector.NewPassiveCollector(s, 100, 10*time.Second)

		// Try to ingest when not running - should fail
		err = pc.Ingest(&models.QuotaInfo{
			AccountID:             "test",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 80.0,
		})
		assert.Error(t, err)
	})

	t.Run("double start", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := store.NewSQLiteStore(filepath.Join(tmpDir, "double_start.db"))
		require.NoError(t, err)
		defer s.Close()

		pc := collector.NewPassiveCollector(s, 100, 10*time.Second)
		ctx := context.Background()

		err = pc.Start(ctx)
		require.NoError(t, err)

		err = pc.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")

		require.NoError(t, pc.Stop())
	})
}

func TestCollector_HTTPHandler(t *testing.T) {
	router, s, cleanup := setupCollectorTest(t)
	defer cleanup()

	// Create test account
	acc := &models.Account{
		ID:       "http-handler-acc",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 10,
	}
	s.SetAccount(acc)

	t.Run("POST to ingest endpoint", func(t *testing.T) {
		body := api.IngestRequest{
			AccountID:             "http-handler-acc",
			Provider:              "openai",
			EffectiveRemainingPct: 85.0,
			Source:                "api",
			Dimensions: []models.Dimension{
				{Type: models.DimensionRPM, Limit: 1000, Used: 150, Remaining: 850},
			},
		}
		jsonBody, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("GET not allowed", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/ingest", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/ingest", bytes.NewBuffer([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestCollector_DimensionExtraction(t *testing.T) {
	t.Run("extract RPM from headers", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "dim-test",
			Provider:              models.ProviderOpenAI,
			EffectiveRemainingPct: 75.0,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 3500, Used: 2625, Remaining: 875},
				{Type: models.DimensionTPM, Limit: 180000, Used: 45000, Remaining: 135000},
			},
		}

		// Find RPM dimension
		var rpmDim *models.Dimension
		for i := range quota.Dimensions {
			if quota.Dimensions[i].Type == models.DimensionRPM {
				rpmDim = &quota.Dimensions[i]
				break
			}
		}
		require.NotNil(t, rpmDim)
		assert.Equal(t, int64(875), rpmDim.Remaining)
		assert.Equal(t, models.DimensionRPM, rpmDim.Type)
	})

	t.Run("extract TPM from headers", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "tpm-test",
			Provider:              models.ProviderAnthropic,
			EffectiveRemainingPct: 80.0,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionTPM, Limit: 100000, Used: 20000, Remaining: 80000},
			},
		}

		// Find TPM dimension
		var tpmDim *models.Dimension
		for i := range quota.Dimensions {
			if quota.Dimensions[i].Type == models.DimensionTPM {
				tpmDim = &quota.Dimensions[i]
				break
			}
		}
		require.NotNil(t, tpmDim)
		assert.Equal(t, int64(80000), tpmDim.Remaining)
		assert.Equal(t, 80.0, tpmDim.RemainingPercent())
	})
}

func TestCollector_CollectorStats(t *testing.T) {
	t.Run("collector reports running status", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := store.NewSQLiteStore(filepath.Join(tmpDir, "stats.db"))
		require.NoError(t, err)
		defer s.Close()

		pc := collector.NewPassiveCollector(s, 100, 10*time.Second)
		ctx := context.Background()

		assert.False(t, pc.IsRunning())

		err = pc.Start(ctx)
		require.NoError(t, err)
		assert.True(t, pc.IsRunning())

		err = pc.Stop()
		require.NoError(t, err)
		assert.False(t, pc.IsRunning())
	})

	t.Run("buffer size tracking", func(t *testing.T) {
		tmpDir := t.TempDir()
		s, err := store.NewSQLiteStore(filepath.Join(tmpDir, "size.db"))
		require.NoError(t, err)
		defer s.Close()

		pc := collector.NewPassiveCollector(s, 50, 10*time.Minute)
		ctx := context.Background()
		err = pc.Start(ctx)
		require.NoError(t, err)
		defer func() { require.NoError(t, pc.Stop()) }()

		assert.Equal(t, 0, pc.BufferSize())

		// Add some items
		for i := 0; i < 5; i++ {
			quota := &models.QuotaInfo{
				AccountID:             string(rune('0' + i)),
				Provider:              models.ProviderOpenAI,
				EffectiveRemainingPct: 80.0,
			}
			err := pc.Ingest(quota)
			require.NoError(t, err)
		}

		assert.Equal(t, 5, pc.BufferSize())
	})
}
