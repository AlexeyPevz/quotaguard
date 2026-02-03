package mocks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// MockHTTPClient implements a mock HTTP client for testing
type MockHTTPClient struct {
	Responses    map[string]*MockResponse
	Requests     []MockRequest
	RequestDelay time.Duration
	mu           sync.Mutex
}

// MockResponse represents a mocked HTTP response
type MockResponse struct {
	StatusCode int
	Body       interface{}
	Headers    map[string]string
}

// MockRequest represents a recorded HTTP request
type MockRequest struct {
	Method  string
	URL     string
	Body    interface{}
	Headers map[string]string
	Time    time.Time
}

// NewMockHTTPClient creates a new mock HTTP client
func NewMockHTTPClient() *MockHTTPClient {
	return &MockHTTPClient{
		Responses: make(map[string]*MockResponse),
		Requests:  make([]MockRequest, 0),
	}
}

// SetResponse sets a mock response for a specific URL pattern
func (m *MockHTTPClient) SetResponse(urlPattern string, response *MockResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[urlPattern] = response
}

// Do executes the mock request
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record request
	mockReq := MockRequest{
		Method:  req.Method,
		URL:     req.URL.String(),
		Time:    time.Now(),
		Headers: make(map[string]string),
	}
	for k, v := range req.Header {
		if len(v) > 0 {
			mockReq.Headers[k] = v[0]
		}
	}
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewBuffer(body))
		mockReq.Body = string(body)
	}
	m.Requests = append(m.Requests, mockReq)

	// Find matching response
	url := req.URL.String()
	response, ok := m.Responses[url]
	if !ok {
		// Try to find a wildcard match
		for pattern, resp := range m.Responses {
			if matchesPattern(url, pattern) {
				response = resp
				break
			}
		}
	}

	if response == nil {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(bytes.NewBufferString(`{"error": "not found"}`)),
		}, nil
	}

	// Apply delay if configured
	if m.RequestDelay > 0 {
		time.Sleep(m.RequestDelay)
	}

	// Create response
	body, _ := json.Marshal(response.Body)
	return &http.Response{
		StatusCode: response.StatusCode,
		Body:       io.NopCloser(bytes.NewBuffer(body)),
		Header:     http.Header{},
	}, nil
}

// GetRequests returns all recorded requests
func (m *MockHTTPClient) GetRequests() []MockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Requests
}

// ClearRequests clears the recorded requests
func (m *MockHTTPClient) ClearRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests = make([]MockRequest, 0)
}

// matchesPattern checks if a URL matches a pattern
func matchesPattern(url, pattern string) bool {
	// Simple pattern matching - can be extended
	if pattern == "*" {
		return true
	}
	return url == pattern
}

// MockQuotaResponse creates a mock quota response for a provider
func MockQuotaResponse(provider models.Provider) *MockResponse {
	switch provider {
	case models.ProviderOpenAI:
		return &MockResponse{
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
		}
	case models.ProviderAnthropic:
		return &MockResponse{
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
		}
	default:
		return &MockResponse{
			StatusCode: 200,
			Body:       map[string]interface{}{},
		}
	}
}

// MockErrorResponse creates a mock error response
func MockErrorResponse(statusCode int, message string) *MockResponse {
	return &MockResponse{
		StatusCode: statusCode,
		Body: map[string]interface{}{
			"error": map[string]interface{}{
				"message": message,
				"type":    "error",
			},
		},
	}
}

// MockRateLimitResponse creates a mock rate limit response
func MockRateLimitResponse() *MockResponse {
	return &MockResponse{
		StatusCode: 429,
		Body: map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
			},
		},
	}
}
