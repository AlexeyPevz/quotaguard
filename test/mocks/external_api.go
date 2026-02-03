package mocks

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// MockExternalAPI simulates external API responses for testing
type MockExternalAPI struct {
	Handlers       map[string]APIHandler
	RequestCount   int
	AverageLatency time.Duration
	mu             sync.Mutex
}

// APIHandler defines a handler for API requests
type APIHandler func(req *APIRequest) (*APIResponse, error)

// APIRequest represents a request to the mock API
type APIRequest struct {
	Method      string
	URL         string
	Headers     map[string]string
	Body        interface{}
	RequestTime time.Time
}

// APIResponse represents a response from the mock API
type APIResponse struct {
	StatusCode int
	Body       interface{}
	Headers    map[string]string
	Latency    time.Duration
}

// NewMockExternalAPI creates a new mock external API
func NewMockExternalAPI() *MockExternalAPI {
	return &MockExternalAPI{
		Handlers:     make(map[string]APIHandler),
		RequestCount: 0,
	}
}

// RegisterHandler registers a handler for a specific URL pattern
func (m *MockExternalAPI) RegisterHandler(urlPattern string, handler APIHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Handlers[urlPattern] = handler
}

// HandleRequest handles a request and returns a mock response
func (m *MockExternalAPI) HandleRequest(ctx context.Context, req *APIRequest) (*APIResponse, error) {
	m.mu.Lock()
	m.RequestCount++
	m.mu.Unlock()

	start := time.Now()

	// Find matching handler
	m.mu.Lock()
	handler, ok := m.Handlers[req.URL]
	m.mu.Unlock()

	if !ok {
		// Return default response
		return &APIResponse{
			StatusCode: 404,
			Body:       map[string]interface{}{"error": "not found"},
			Latency:    10 * time.Millisecond,
		}, nil
	}

	response, err := handler(req)
	if err != nil {
		return nil, err
	}

	response.Latency = time.Since(start)
	m.mu.Lock()
	m.AverageLatency = (m.AverageLatency*time.Duration(m.RequestCount-1) + response.Latency) / time.Duration(m.RequestCount)
	m.mu.Unlock()

	return response, nil
}

// GetRequestCount returns the number of requests handled
func (m *MockExternalAPI) GetRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RequestCount
}

// ResetRequestCount resets the request count
func (m *MockExternalAPI) ResetRequestCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RequestCount = 0
}

// OpenAIHandler creates a handler for OpenAI API
func OpenAIHandler() APIHandler {
	return func(req *APIRequest) (*APIResponse, error) {
		return &APIResponse{
			StatusCode: 200,
			Body: map[string]interface{}{
				"data": map[string]interface{}{
					"rate_limit": map[string]interface{}{
						"requests": map[string]interface{}{
							"limit":  3500,
							"used":   700,
							"remain": 2800,
						},
						"tokens": map[string]interface{}{
							"limit":  100000,
							"used":   20000,
							"remain": 80000,
						},
					},
				},
			},
		}, nil
	}
}

// AnthropicHandler creates a handler for Anthropic API
func AnthropicHandler() APIHandler {
	return func(req *APIRequest) (*APIResponse, error) {
		return &APIResponse{
			StatusCode: 200,
			Body: map[string]interface{}{
				"rate_limits": []map[string]interface{}{
					{
						"limit":     1000,
						"used":      100,
						"remaining": 900,
						"unit":      "requests",
					},
				},
			},
		}, nil
	}
}

// RateLimitedHandler returns a rate limited response
func RateLimitedHandler() APIHandler {
	return func(req *APIRequest) (*APIResponse, error) {
		return &APIResponse{
			StatusCode: 429,
			Body: map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Rate limit exceeded",
					"type":    "rate_limit_error",
				},
			},
		}, nil
	}
}

// ErrorHandler creates a handler that returns errors
func ErrorHandler(statusCode int, message string) APIHandler {
	return func(req *APIRequest) (*APIResponse, error) {
		return &APIResponse{
			StatusCode: statusCode,
			Body: map[string]interface{}{
				"error": map[string]interface{}{
					"message": message,
					"type":    "api_error",
				},
			},
		}, nil
	}
}

// QuotaInfoFromResponse extracts quota info from an API response
func QuotaInfoFromResponse(provider models.Provider, body []byte) (*models.QuotaInfo, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	quota := &models.QuotaInfo{
		Provider:              provider,
		EffectiveRemainingPct: 100.0,
		Dimensions:            make(models.DimensionSlice, 0),
		CollectedAt:           time.Now(),
	}

	// Parse provider-specific response
	switch provider {
	case models.ProviderOpenAI:
		if dataMap, ok := data["data"].(map[string]interface{}); ok {
			if rateLimit, ok := dataMap["rate_limit"].(map[string]interface{}); ok {
				if requests, ok := rateLimit["requests"].(map[string]interface{}); ok {
					limit := int64(requests["limit"].(float64))
					used := int64(requests["used"].(float64))
					remaining := int64(requests["remain"].(float64))
					quota.Dimensions = append(quota.Dimensions, models.Dimension{
						Type:      models.DimensionRPM,
						Limit:     limit,
						Used:      used,
						Remaining: remaining,
					})
				}
				if tokens, ok := rateLimit["tokens"].(map[string]interface{}); ok {
					limit := int64(tokens["limit"].(float64))
					used := int64(tokens["used"].(float64))
					remaining := int64(tokens["remain"].(float64))
					quota.Dimensions = append(quota.Dimensions, models.Dimension{
						Type:      models.DimensionTPM,
						Limit:     limit,
						Used:      used,
						Remaining: remaining,
					})
				}
			}
		}
	case models.ProviderAnthropic:
		if rateLimits, ok := data["rate_limits"].([]interface{}); ok {
			for _, rl := range rateLimits {
				if rlMap, ok := rl.(map[string]interface{}); ok {
					dim := models.Dimension{
						Limit:     int64(rlMap["limit"].(float64)),
						Used:      int64(rlMap["used"].(float64)),
						Remaining: int64(rlMap["remaining"].(float64)),
					}
					if unit, ok := rlMap["unit"].(string); ok {
						switch unit {
						case "requests":
							dim.Type = models.DimensionRPM
						case "tokens":
							dim.Type = models.DimensionTPM
						}
					}
					quota.Dimensions = append(quota.Dimensions, dim)
				}
			}
		}
	}

	// Calculate effective remaining percentage
	if len(quota.Dimensions) > 0 {
		quota.EffectiveRemainingPct = quota.Dimensions.MinRemainingPercent()
	}

	return quota, nil
}

// SimulateNetworkDelay simulates network delay
func SimulateNetworkDelay(delay time.Duration) {
	time.Sleep(delay)
}
