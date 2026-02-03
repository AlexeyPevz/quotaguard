package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
)

// setupBenchServer creates a test server for benchmarks.
func setupBenchServer(b *testing.B) (*Server, *store.MemoryStore) {
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

// setupBenchServerWithData creates a test server with pre-populated data.
func setupBenchServerWithData(b *testing.B) (*Server, *store.MemoryStore) {
	s := store.NewMemoryStore()

	// Pre-populate with test accounts
	for i := 0; i < 100; i++ {
		acc := &models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			Priority:         100 - i%10,
			ConcurrencyLimit: 10,
			InputCost:        0.01,
			OutputCost:       0.03,
		}
		s.SetAccount(acc)
		s.SetQuota(fmt.Sprintf("account-%d", i), &models.QuotaInfo{
			AccountID:             fmt.Sprintf("account-%d", i),
			EffectiveRemainingPct: float64(100 - i%20),
			Confidence:            0.9,
			Provider:              models.ProviderOpenAI,
		})
	}

	gin.SetMode(gin.TestMode)
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

// BenchmarkHandleHealth benchmarks the health check endpoint.
func BenchmarkHandleHealth(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServer(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Create a test request
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleRouterSelect benchmarks the router select endpoint.
func BenchmarkHandleRouterSelect(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServerWithData(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Create request body
	body := router.SelectRequest{
		Provider: models.ProviderOpenAI,
	}
	bodyBytes, _ := json.Marshal(body)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/route/select", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleListQuotas benchmarks the list quotas endpoint.
func BenchmarkHandleListQuotas(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServerWithData(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/quotas", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleCreateReservation benchmarks the create reservation endpoint.
func BenchmarkHandleCreateReservation(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServerWithData(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Create request body
	reservation := struct {
		AccountID     string  `json:"account_id"`
		CorrelationID string  `json:"correlation_id"`
		EstimatedCost float64 `json:"estimated_cost_percent"`
	}{
		AccountID:     "account-0",
		CorrelationID: "test-correlation",
		EstimatedCost: 5.0,
	}
	bodyBytes, _ := json.Marshal(reservation)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/reservations", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleIngest benchmarks the quota ingest endpoint.
func BenchmarkHandleIngest(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServer(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Create ingest request body
	ingest := struct {
		AccountID             string  `json:"account_id"`
		EffectiveRemainingPct float64 `json:"effective_remaining_percent"`
	}{
		AccountID:             "account-0",
		EffectiveRemainingPct: 75.0,
	}
	bodyBytes, _ := json.Marshal(ingest)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleIngestWithMultipleAccounts benchmarks the quota ingest with multiple accounts.
func BenchmarkHandleIngestWithMultipleAccounts(b *testing.B) {
	b.ReportAllocs()

	server, s := setupBenchServer(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Pre-create accounts
	for i := 0; i < 50; i++ {
		s.SetAccount(&models.Account{
			ID:       fmt.Sprintf("account-%d", i),
			Provider: models.ProviderOpenAI,
			Enabled:  true,
			Priority: 100,
		})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ingest := struct {
			AccountID             string  `json:"account_id"`
			EffectiveRemainingPct float64 `json:"effective_remaining_percent"`
		}{
			AccountID:             fmt.Sprintf("account-%d", i%50),
			EffectiveRemainingPct: float64(100 - i%20),
		}
		bodyBytes, _ := json.Marshal(ingest)

		req, _ := http.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleRouterSelectConcurrent benchmarks concurrent router select requests.
func BenchmarkHandleRouterSelectConcurrent(b *testing.B) {
	b.ReportAllocs()
	b.SetParallelism(10)

	server, _ := setupBenchServerWithData(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			body := router.SelectRequest{
				Provider: models.ProviderOpenAI,
			}
			bodyBytes, _ := json.Marshal(body)

			req, _ := http.NewRequest(http.MethodPost, "/api/v1/route/select", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.Router().ServeHTTP(w, req)
		}
	})
}

// BenchmarkHandleMetricsEndpoint benchmarks the metrics endpoint.
func BenchmarkHandleMetricsEndpoint(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServer(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleListAccounts benchmarks the list accounts endpoint.
func BenchmarkHandleListAccounts(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServerWithData(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// BenchmarkHandleAccountSelectByID benchmarks selecting a specific account.
func BenchmarkHandleAccountSelectByID(b *testing.B) {
	b.ReportAllocs()

	server, _ := setupBenchServerWithData(b)
	defer func() { _ = server.Shutdown(context.Background()) }()

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/route/select/account-0", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		server.Router().ServeHTTP(w, req)
	}
}

// Note: These benchmarks can be run with:
// go test -bench=BenchmarkHandle -benchmem -run=^$ ./internal/api/
