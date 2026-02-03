package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewCLIProxyAPIClient(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		opts        []ClientOption
		wantKey     string
		wantTimeout time.Duration
	}{
		{
			name:        "default client",
			baseURL:     "http://localhost:8080",
			wantTimeout: 30 * time.Second,
		},
		{
			name:        "with API key",
			baseURL:     "http://localhost:8080",
			opts:        []ClientOption{WithAPIKey("test-key-123")},
			wantKey:     "test-key-123",
			wantTimeout: 30 * time.Second,
		},
		{
			name:        "with custom timeout",
			baseURL:     "http://localhost:8080",
			opts:        []ClientOption{WithTimeout(10 * time.Second)},
			wantTimeout: 10 * time.Second,
		},
		{
			name:    "with custom HTTP client",
			baseURL: "http://localhost:8080",
			opts: []ClientOption{
				WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
			},
			wantTimeout: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewCLIProxyAPIClient(tt.baseURL, tt.opts...)

			if client.baseURL != tt.baseURL {
				t.Errorf("baseURL = %v, want %v", client.baseURL, tt.baseURL)
			}

			if client.apiKey != tt.wantKey {
				t.Errorf("apiKey = %v, want %v", client.apiKey, tt.wantKey)
			}

			if client.httpClient.Timeout != tt.wantTimeout {
				t.Errorf("timeout = %v, want %v", client.httpClient.Timeout, tt.wantTimeout)
			}
		})
	}
}

func TestCLIProxyAPIClient_ForwardRequest(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		expectedPath := "/v1/proxy/openai/acc123"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type header to be application/json")
		}

		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key header to be test-key")
		}

		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("expected X-Custom-Header to be custom-value")
		}

		// Read and verify body
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"test":"data"}` {
			t.Errorf("expected body {\"test\":\"data\"}, got %s", string(body))
		}

		// Send response
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL, WithAPIKey("test-key"))

	ctx := context.Background()
	headers := map[string]string{
		"X-Custom-Header": "custom-value",
	}

	resp, err := client.ForwardRequest(ctx, "acc123", "openai", []byte(`{"test":"data"}`), headers)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestCLIProxyAPIClient_ForwardRequest_Error(t *testing.T) {
	client := NewCLIProxyAPIClient("http://invalid-url-that-does-not-exist:99999")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.ForwardRequest(ctx, "acc123", "openai", []byte(`{}`), nil)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestCLIProxyAPIClient_Health(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("expected path /health, got %s", r.URL.Path)
		}

		response := HealthResponse{
			Status:    "healthy",
			Version:   "1.0.0",
			Timestamp: time.Now(),
			Checks: map[string]string{
				"database": "ok",
				"cache":    "ok",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, response)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if health.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", health.Status)
	}

	if health.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", health.Version)
	}

	if health.Checks["database"] != "ok" {
		t.Errorf("expected database check to be ok")
	}
}

func TestCLIProxyAPIClient_Health_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	_, err := client.Health(ctx)
	if err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}

func TestCLIProxyAPIClient_Health_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`invalid json`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	_, err := client.Health(ctx)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestCLIProxyAPIClient_Metrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			t.Errorf("expected path /metrics, got %s", r.URL.Path)
		}

		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key header")
		}

		response := MetricsResponse{
			RequestsTotal:  1000,
			RequestsActive: 5,
			ErrorsTotal:    10,
			LatencyAvg:     50 * time.Millisecond,
			LatencyP95:     100 * time.Millisecond,
			LatencyP99:     150 * time.Millisecond,
			ByProvider: map[string]int64{
				"openai":    800,
				"anthropic": 200,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, response)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL, WithAPIKey("test-key"))

	ctx := context.Background()
	metrics, err := client.Metrics(ctx)
	if err != nil {
		t.Fatalf("Metrics failed: %v", err)
	}

	if metrics.RequestsTotal != 1000 {
		t.Errorf("expected RequestsTotal 1000, got %d", metrics.RequestsTotal)
	}

	if metrics.RequestsActive != 5 {
		t.Errorf("expected RequestsActive 5, got %d", metrics.RequestsActive)
	}

	if metrics.ErrorsTotal != 10 {
		t.Errorf("expected ErrorsTotal 10, got %d", metrics.ErrorsTotal)
	}

	if metrics.ByProvider["openai"] != 800 {
		t.Errorf("expected openai requests 800, got %d", metrics.ByProvider["openai"])
	}
}

func TestCLIProxyAPIClient_Metrics_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	_, err := client.Metrics(ctx)
	if err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}

func TestCLIProxyAPIClient_GetQuotaStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/v1/quota/acc123"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key header")
		}

		response := QuotaStatus{
			AccountID:         "acc123",
			Provider:          "openai",
			RequestsUsed:      500,
			RequestsRemaining: 500,
			TokensUsed:        10000,
			TokensRemaining:   90000,
			ResetsAt:          time.Now().Add(24 * time.Hour),
		}

		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, response)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL, WithAPIKey("test-key"))

	ctx := context.Background()
	status, err := client.GetQuotaStatus(ctx, "acc123")
	if err != nil {
		t.Fatalf("GetQuotaStatus failed: %v", err)
	}

	if status.AccountID != "acc123" {
		t.Errorf("expected AccountID acc123, got %s", status.AccountID)
	}

	if status.RequestsUsed != 500 {
		t.Errorf("expected RequestsUsed 500, got %d", status.RequestsUsed)
	}

	if status.RequestsRemaining != 500 {
		t.Errorf("expected RequestsRemaining 500, got %d", status.RequestsRemaining)
	}
}

func TestCLIProxyAPIClient_GetQuotaStatus_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	_, err := client.GetQuotaStatus(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for 404 status, got nil")
	}
}

func TestCLIProxyAPIClient_UpdateQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/v1/quota/acc123"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key header")
		}

		// Verify request body
		var req UpdateQuotaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if req.RequestsUsed != 10 {
			t.Errorf("expected RequestsUsed 10, got %d", req.RequestsUsed)
		}

		if req.TokensUsed != 100 {
			t.Errorf("expected TokensUsed 100, got %d", req.TokensUsed)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL, WithAPIKey("test-key"))

	ctx := context.Background()
	err := client.UpdateQuota(ctx, "acc123", UpdateQuotaRequest{
		RequestsUsed: 10,
		TokensUsed:   100,
	})
	if err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}
}

func TestCLIProxyAPIClient_UpdateQuota_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		mustWrite(t, w, []byte(`{"error":"invalid request"}`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	err := client.UpdateQuota(ctx, "acc123", UpdateQuotaRequest{
		RequestsUsed: 10,
	})
	if err == nil {
		t.Error("expected error for non-2xx status, got nil")
	}
}

func TestCLIProxyAPIClient_Close(t *testing.T) {
	client := NewCLIProxyAPIClient("http://localhost:8080")

	err := client.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestCLIProxyAPIClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.Health(ctx)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestCLIProxyAPIClient_ConcurrentRequests(t *testing.T) {
	var requestCount int64
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	// Run concurrent requests
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			ctx := context.Background()
			_, err := client.Health(ctx)
			if err != nil {
				t.Errorf("Health request failed: %v", err)
			}
			done <- true
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()
	if finalCount != 10 {
		t.Errorf("expected 10 requests, got %d", finalCount)
	}
}

func TestCLIProxyAPIClient_WithTimeout(t *testing.T) {
	client := NewCLIProxyAPIClient("http://localhost:8080", WithTimeout(5*time.Second))

	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", client.httpClient.Timeout)
	}
}

func TestCLIProxyAPIClient_WithHTTPClient(t *testing.T) {
	customClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns: 100,
		},
	}

	client := NewCLIProxyAPIClient("http://localhost:8080", WithHTTPClient(customClient))

	if client.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}

	if client.httpClient.Timeout != 15*time.Second {
		t.Errorf("expected timeout 15s, got %v", client.httpClient.Timeout)
	}
}

func TestCLIProxyAPIClient_WithAPIKey(t *testing.T) {
	client := NewCLIProxyAPIClient("http://localhost:8080", WithAPIKey("my-secret-key"))

	if client.apiKey != "my-secret-key" {
		t.Errorf("expected API key my-secret-key, got %s", client.apiKey)
	}
}

func TestCLIProxyAPIClient_MultipleOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 20 * time.Second}

	client := NewCLIProxyAPIClient(
		"http://localhost:8080",
		WithAPIKey("key123"),
		WithHTTPClient(customClient),
		WithTimeout(25*time.Second), // This should override the custom client's timeout
	)

	if client.apiKey != "key123" {
		t.Errorf("expected API key key123, got %s", client.apiKey)
	}

	if client.httpClient.Timeout != 25*time.Second {
		t.Errorf("expected timeout 25s, got %v", client.httpClient.Timeout)
	}
}

func TestCLIProxyAPIClient_ForwardRequest_NoHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	resp, err := client.ForwardRequest(ctx, "acc123", "openai", []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestCLIProxyAPIClient_Metrics_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`invalid json`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	_, err := client.Metrics(ctx)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestCLIProxyAPIClient_GetQuotaStatus_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`invalid json`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	_, err := client.GetQuotaStatus(ctx, "acc123")
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestCLIProxyAPIClient_UpdateQuota_InvalidURL(t *testing.T) {
	client := NewCLIProxyAPIClient("http://[invalid-url")

	ctx := context.Background()
	err := client.UpdateQuota(ctx, "acc123", UpdateQuotaRequest{RequestsUsed: 10})
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestCLIProxyAPIClient_ForwardRequest_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.ForwardRequest(ctx, "acc123", "openai", []byte(`{}`), nil)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestCLIProxyAPIClient_UpdateQuota_OKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`{"status":"updated"}`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	err := client.UpdateQuota(ctx, "acc123", UpdateQuotaRequest{RequestsUsed: 10})
	if err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}
}

func TestCLIProxyAPIClient_ForwardRequest_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("expected empty body, got %s", string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	resp, err := client.ForwardRequest(ctx, "acc123", "openai", []byte{}, nil)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestCLIProxyAPIClient_Metrics_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`{}`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	metrics, err := client.Metrics(ctx)
	if err != nil {
		t.Fatalf("Metrics failed: %v", err)
	}

	if metrics.RequestsTotal != 0 {
		t.Errorf("expected RequestsTotal 0, got %d", metrics.RequestsTotal)
	}
}

func TestCLIProxyAPIClient_GetQuotaStatus_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustWrite(t, w, []byte(`{}`))
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	status, err := client.GetQuotaStatus(ctx, "acc123")
	if err != nil {
		t.Fatalf("GetQuotaStatus failed: %v", err)
	}

	if status.AccountID != "" {
		t.Errorf("expected empty AccountID, got %s", status.AccountID)
	}
}

func TestCLIProxyAPIClient_Health_EmptyChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := HealthResponse{
			Status:  "healthy",
			Version: "1.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, response)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if health.Checks != nil {
		t.Errorf("expected nil Checks, got %v", health.Checks)
	}
}

func TestCLIProxyAPIClient_ForwardRequest_LargeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Error("expected non-empty body")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	largeBody := make([]byte, 1024*1024) // 1MB
	for i := range largeBody {
		largeBody[i] = 'a'
	}

	resp, err := client.ForwardRequest(ctx, "acc123", "openai", largeBody, nil)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestCLIProxyAPIClient_ConcurrentHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		mustEncode(t, w, HealthResponse{Status: "healthy"})
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			_, err := client.Health(ctx)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		if err != nil {
			errorCount++
			t.Logf("Error: %v", err)
		}
	}

	if errorCount > 0 {
		t.Errorf("got %d errors during concurrent health checks", errorCount)
	}
}

func TestCLIProxyAPIClient_ForwardRequest_WithMultipleHeaders(t *testing.T) {
	receivedHeaders := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for key := range r.Header {
			receivedHeaders[key] = r.Header.Get(key)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL, WithAPIKey("test-key"))

	ctx := context.Background()
	headers := map[string]string{
		"X-Custom-1": "value1",
		"X-Custom-2": "value2",
		"X-Custom-3": "value3",
	}

	resp, err := client.ForwardRequest(ctx, "acc123", "openai", []byte(`{}`), headers)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if receivedHeaders["X-Custom-1"] != "value1" {
		t.Errorf("expected X-Custom-1 header to be value1")
	}
	if receivedHeaders["X-Custom-2"] != "value2" {
		t.Errorf("expected X-Custom-2 header to be value2")
	}
	if receivedHeaders["X-Custom-3"] != "value3" {
		t.Errorf("expected X-Custom-3 header to be value3")
	}
	if receivedHeaders["X-Api-Key"] != "test-key" {
		t.Errorf("expected X-API-Key header to be test-key")
	}
}

func TestCLIProxyAPIClient_Metrics_WithNilMaps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := MetricsResponse{
			RequestsTotal: 100,
			// ByProvider and ByAccount are nil
		}
		w.Header().Set("Content-Type", "application/json")
		mustEncode(t, w, response)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	metrics, err := client.Metrics(ctx)
	if err != nil {
		t.Fatalf("Metrics failed: %v", err)
	}

	if metrics.RequestsTotal != 100 {
		t.Errorf("expected RequestsTotal 100, got %d", metrics.RequestsTotal)
	}

	if metrics.ByProvider != nil {
		t.Errorf("expected nil ByProvider, got %v", metrics.ByProvider)
	}
}

func TestCLIProxyAPIClient_UpdateQuota_ZeroValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req UpdateQuotaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.RequestsUsed != 0 {
			t.Errorf("expected RequestsUsed 0, got %d", req.RequestsUsed)
		}
		if req.TokensUsed != 0 {
			t.Errorf("expected TokensUsed 0, got %d", req.TokensUsed)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewCLIProxyAPIClient(server.URL)

	ctx := context.Background()
	err := client.UpdateQuota(ctx, "acc123", UpdateQuotaRequest{})
	if err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}
}

func TestCLIProxyAPIClient_QuotaStatus_ErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"bad request", http.StatusBadRequest, true},
		{"unauthorized", http.StatusUnauthorized, true},
		{"forbidden", http.StatusForbidden, true},
		{"internal error", http.StatusInternalServerError, true},
		{"service unavailable", http.StatusServiceUnavailable, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewCLIProxyAPIClient(server.URL)

			ctx := context.Background()
			_, err := client.GetQuotaStatus(ctx, "acc123")
			if (err != nil) != tt.wantErr {
				t.Errorf("GetQuotaStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCLIProxyAPIClient_ForwardRequest_ErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"bad gateway", http.StatusBadGateway},
		{"gateway timeout", http.StatusGatewayTimeout},
		{"too many requests", http.StatusTooManyRequests},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewCLIProxyAPIClient(server.URL)

			ctx := context.Background()
			resp, err := client.ForwardRequest(ctx, "acc123", "openai", []byte(`{}`), nil)
			if err != nil {
				t.Fatalf("ForwardRequest failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, resp.StatusCode)
			}
		})
	}
}

func mustWrite(t *testing.T, w http.ResponseWriter, payload []byte) {
	t.Helper()
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("write response failed: %v", err)
	}
}

func mustEncode(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response failed: %v", err)
	}
}
