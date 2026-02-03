package health

import (
	"context"
	"net/http"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// ProviderClient checks the availability of LLM providers.
type ProviderClient struct {
	httpClient *http.Client
	endpoints  map[models.Provider]string
	timeout    time.Duration
}

// HealthResult represents the result of a provider health check.
type HealthResult struct {
	Latency    time.Duration
	StatusCode int
	Error      error
	Available  bool
	Message    string
}

// NewProviderClient creates a new provider client.
func NewProviderClient(timeout time.Duration) *ProviderClient {
	return &ProviderClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		endpoints: make(map[models.Provider]string),
		timeout:   timeout,
	}
}

// AddEndpoint adds an endpoint for a provider.
func (c *ProviderClient) AddEndpoint(provider models.Provider, endpoint string) {
	c.endpoints[provider] = endpoint
}

// SetEndpoints sets multiple endpoints at once.
func (c *ProviderClient) SetEndpoints(endpoints map[models.Provider]string) {
	for provider, endpoint := range endpoints {
		c.endpoints[provider] = endpoint
	}
}

// CheckHealth checks the availability of a specific provider.
func (c *ProviderClient) CheckHealth(ctx context.Context, provider models.Provider) (*HealthResult, error) {
	endpoint, ok := c.endpoints[provider]
	if !ok {
		return &HealthResult{
			Available: false,
			Message:   "no endpoint configured",
		}, nil
	}

	start := time.Now()

	// Use HEAD request to check availability
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint+"/models", nil)
	if err != nil {
		return &HealthResult{
			Latency:   time.Since(start),
			Available: false,
			Error:     err,
			Message:   "failed to create request",
		}, nil
	}

	// Add provider-specific headers
	c.addProviderHeaders(req, provider)

	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)

	if err != nil {
		return &HealthResult{
			Latency:   latency,
			Available: false,
			Error:     err,
			Message:   "request failed",
		}, nil
	}
	defer resp.Body.Close()

	// Consider 2xx and 401 (auth error, but service is reachable) as available
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return &HealthResult{
			Latency:    latency,
			StatusCode: resp.StatusCode,
			Available:  true,
			Message:    "provider is reachable",
		}, nil
	}

	return &HealthResult{
		Latency:    latency,
		StatusCode: resp.StatusCode,
		Available:  false,
		Message:    "unexpected status code",
	}, nil
}

// CheckAll checks the health of all configured providers.
func (c *ProviderClient) CheckAll(ctx context.Context) map[models.Provider]*HealthResult {
	results := make(map[models.Provider]*HealthResult)

	for provider := range c.endpoints {
		result, err := c.CheckHealth(ctx, provider)
		if err != nil {
			results[provider] = &HealthResult{
				Available: false,
				Error:     err,
				Message:   "check failed",
			}
		} else {
			results[provider] = result
		}
	}

	return results
}

// GetEndpoint returns the endpoint for a provider.
func (c *ProviderClient) GetEndpoint(provider models.Provider) (string, bool) {
	endpoint, ok := c.endpoints[provider]
	return endpoint, ok
}

// addProviderHeaders adds provider-specific headers to the request.
func (c *ProviderClient) addProviderHeaders(req *http.Request, provider models.Provider) {
	switch provider {
	case models.ProviderAnthropic:
		req.Header.Set("x-api-key", "dummy")
		req.Header.Set("anthropic-version", "2023-06-01")
	case models.ProviderOpenAI:
		req.Header.Set("Authorization", "Bearer dummy")
	case models.ProviderGemini:
		req.Header.Set("Content-Type", "application/json")
	case models.ProviderAzure:
		req.Header.Set("api-key", "dummy")
	}
}

// DefaultProviderEndpoints returns the default endpoints for known providers.
func DefaultProviderEndpoints() map[models.Provider]string {
	return map[models.Provider]string{
		models.ProviderOpenAI:    "https://api.openai.com/v1",
		models.ProviderAnthropic: "https://api.anthropic.com/v1",
		models.ProviderGemini:    "https://generativelanguage.googleapis.com/v1",
		// Azure endpoint is dynamic, so we don't set a default here
	}
}
