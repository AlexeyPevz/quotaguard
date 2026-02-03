package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MaxResponseBodySize is the maximum size of response body to read (1MB)
const MaxResponseBodySize = 1 << 20 // 1MB

// CLIProxyAPIClient is a client for the CLI Proxy API middleware.
// It provides methods to interact with the middleware for request forwarding,
// health checks, and metrics collection.
type CLIProxyAPIClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// ClientOption is a functional option for configuring CLIProxyAPIClient.
type ClientOption func(*CLIProxyAPIClient)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *CLIProxyAPIClient) {
		c.httpClient = client
	}
}

// WithAPIKey sets the API key for authentication.
func WithAPIKey(key string) ClientOption {
	return func(c *CLIProxyAPIClient) {
		c.apiKey = key
	}
}

// WithTimeout sets the timeout for HTTP requests.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *CLIProxyAPIClient) {
		c.httpClient.Timeout = timeout
	}
}

// NewCLIProxyAPIClient creates a new CLI Proxy API client.
func NewCLIProxyAPIClient(baseURL string, opts ...ClientOption) *CLIProxyAPIClient {
	client := &CLIProxyAPIClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// ForwardRequest forwards a request to the target provider through the middleware.
func (c *CLIProxyAPIClient) ForwardRequest(ctx context.Context, accountID, provider string, body []byte, headers map[string]string) (*http.Response, error) {
	url := fmt.Sprintf("%s/v1/proxy/%s/%s", c.baseURL, provider, accountID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Timestamp time.Time         `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// Health performs a health check on the middleware.
func (c *CLIProxyAPIClient) Health(ctx context.Context) (*HealthResponse, error) {
	url := fmt.Sprintf("%s/health", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode health response: %w", err)
	}

	return &health, nil
}

// MetricsResponse represents the metrics response.
type MetricsResponse struct {
	RequestsTotal  int64            `json:"requests_total"`
	RequestsActive int64            `json:"requests_active"`
	ErrorsTotal    int64            `json:"errors_total"`
	LatencyAvg     time.Duration    `json:"latency_avg_ms"`
	LatencyP95     time.Duration    `json:"latency_p95_ms"`
	LatencyP99     time.Duration    `json:"latency_p99_ms"`
	ByProvider     map[string]int64 `json:"by_provider,omitempty"`
	ByAccount      map[string]int64 `json:"by_account,omitempty"`
}

// Metrics retrieves metrics from the middleware.
func (c *CLIProxyAPIClient) Metrics(ctx context.Context) (*MetricsResponse, error) {
	url := fmt.Sprintf("%s/metrics", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics request returned status %d", resp.StatusCode)
	}

	var metrics MetricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("failed to decode metrics response: %w", err)
	}

	return &metrics, nil
}

// QuotaStatus represents the quota status for an account.
type QuotaStatus struct {
	AccountID         string    `json:"account_id"`
	Provider          string    `json:"provider"`
	RequestsUsed      int64     `json:"requests_used"`
	RequestsRemaining int64     `json:"requests_remaining"`
	TokensUsed        int64     `json:"tokens_used"`
	TokensRemaining   int64     `json:"tokens_remaining"`
	ResetsAt          time.Time `json:"resets_at"`
}

// GetQuotaStatus retrieves the quota status for an account.
func (c *CLIProxyAPIClient) GetQuotaStatus(ctx context.Context, accountID string) (*QuotaStatus, error) {
	url := fmt.Sprintf("%s/v1/quota/%s", c.baseURL, accountID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("quota status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("quota status request returned status %d", resp.StatusCode)
	}

	var status QuotaStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode quota status: %w", err)
	}

	return &status, nil
}

// UpdateQuotaRequest represents a request to update quota.
type UpdateQuotaRequest struct {
	RequestsUsed int64 `json:"requests_used,omitempty"`
	TokensUsed   int64 `json:"tokens_used,omitempty"`
}

// UpdateQuota updates the quota for an account.
func (c *CLIProxyAPIClient) UpdateQuota(ctx context.Context, accountID string, req UpdateQuotaRequest) error {
	url := fmt.Sprintf("%s/v1/quota/%s", c.baseURL, accountID)

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("update quota request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		// Limit body read to prevent DoS
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBodySize))
		return fmt.Errorf("update quota request returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// Close closes the client and cleans up resources.
func (c *CLIProxyAPIClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}
