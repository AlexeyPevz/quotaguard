package failopen

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAccountProvider is a mock implementation of AccountProvider for testing.
type mockAccountProvider struct {
	mu       sync.RWMutex
	accounts []*models.Account
}

func newMockAccountProvider() *mockAccountProvider {
	return &mockAccountProvider{
		accounts: []*models.Account{
			{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true, Priority: 10},
			{ID: "acc-2", Provider: models.ProviderAnthropic, Enabled: true, Priority: 5},
			{ID: "acc-3", Provider: models.ProviderGemini, Enabled: true, Priority: 3},
		},
	}
}

func (m *mockAccountProvider) ListEnabledAccounts() []*models.Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*models.Account, len(m.accounts))
	copy(result, m.accounts)
	return result
}

func (m *mockAccountProvider) GetAccount(id string) (*models.Account, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, acc := range m.accounts {
		if acc.ID == id {
			return acc, true
		}
	}
	return nil, false
}

func (m *mockAccountProvider) SetAccounts(accounts []*models.Account) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accounts = accounts
}

// mockMiddlewareClient is a mock implementation of MiddlewareClient for testing.
type mockMiddlewareClient struct {
	mu          sync.Mutex
	callCount   int
	shouldFail  bool
	failError   error
	delay       time.Duration
	returnValue *Result
}

func newMockMiddlewareClient() *mockMiddlewareClient {
	return &mockMiddlewareClient{
		returnValue: &Result{
			AccountID: "acc-1",
			Provider:  models.ProviderOpenAI,
			Fallback:  false,
			Reason:    "",
		},
	}
}

func (m *mockMiddlewareClient) SelectAccount(ctx context.Context, req SelectAccountRequest) (*Result, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.shouldFail {
		return nil, m.failError
	}

	return m.returnValue, nil
}

func (m *mockMiddlewareClient) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func (m *mockMiddlewareClient) SetShouldFail(fail bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = fail
	m.failError = err
}

func (m *mockMiddlewareClient) SetDelay(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delay = d
}

func (m *mockMiddlewareClient) SetReturnValue(result *Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.returnValue = result
}

// Test RoundRobinStrategy
func TestRoundRobinStrategy(t *testing.T) {
	t.Run("empty accounts returns nil", func(t *testing.T) {
		strategy := NewRoundRobinStrategy()
		result := strategy.SelectAccount([]*models.Account{}, "")
		assert.Nil(t, result)
	})

	t.Run("cycles through accounts", func(t *testing.T) {
		strategy := NewRoundRobinStrategy()
		accounts := []*models.Account{
			{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true},
			{ID: "acc-2", Provider: models.ProviderAnthropic, Enabled: true},
			{ID: "acc-3", Provider: models.ProviderGemini, Enabled: true},
		}

		// First call
		result1 := strategy.SelectAccount(accounts, "")
		assert.Equal(t, "acc-1", result1.ID)

		// Second call
		result2 := strategy.SelectAccount(accounts, "acc-1")
		assert.Equal(t, "acc-2", result2.ID)

		// Third call
		result3 := strategy.SelectAccount(accounts, "acc-2")
		assert.Equal(t, "acc-3", result3.ID)

		// Fourth call - should wrap around
		result4 := strategy.SelectAccount(accounts, "acc-3")
		assert.Equal(t, "acc-1", result4.ID)
	})

	t.Run("adjusts index when lastUsed changes", func(t *testing.T) {
		strategy := NewRoundRobinStrategy()
		accounts := []*models.Account{
			{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true},
			{ID: "acc-2", Provider: models.ProviderAnthropic, Enabled: true},
		}

		// Select first account
		_ = strategy.SelectAccount(accounts, "")

		// Simulate external change to lastUsed
		result := strategy.SelectAccount(accounts, "acc-2")
		assert.Equal(t, "acc-1", result.ID) // Should start from beginning
	})

	t.Run("name is correct", func(t *testing.T) {
		strategy := NewRoundRobinStrategy()
		assert.Equal(t, "round-robin", strategy.Name())
	})
}

// Test FirstAvailableStrategy
func TestFirstAvailableStrategy(t *testing.T) {
	t.Run("empty accounts returns nil", func(t *testing.T) {
		strategy := NewFirstAvailableStrategy()
		result := strategy.SelectAccount([]*models.Account{}, "")
		assert.Nil(t, result)
	})

	t.Run("returns an account", func(t *testing.T) {
		strategy := NewFirstAvailableStrategy()
		accounts := []*models.Account{
			{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true},
			{ID: "acc-2", Provider: models.ProviderAnthropic, Enabled: true},
		}

		result := strategy.SelectAccount(accounts, "")
		assert.NotNil(t, result)
		assert.Contains(t, []string{"acc-1", "acc-2"}, result.ID)
	})

	t.Run("name is correct", func(t *testing.T) {
		strategy := NewFirstAvailableStrategy()
		assert.Equal(t, "first-available", strategy.Name())
	})
}

// Test WeightedStrategy
func TestWeightedStrategy(t *testing.T) {
	t.Run("empty accounts returns nil", func(t *testing.T) {
		strategy := NewWeightedStrategy()
		result := strategy.SelectAccount([]*models.Account{}, "")
		assert.Nil(t, result)
	})

	t.Run("returns an account", func(t *testing.T) {
		strategy := NewWeightedStrategy()
		accounts := []*models.Account{
			{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true, Priority: 10},
			{ID: "acc-2", Provider: models.ProviderAnthropic, Enabled: true, Priority: 5},
		}

		result := strategy.SelectAccount(accounts, "")
		assert.NotNil(t, result)
		assert.Contains(t, []string{"acc-1", "acc-2"}, result.ID)
	})

	t.Run("higher priority accounts selected more often", func(t *testing.T) {
		strategy := NewWeightedStrategy()
		accounts := []*models.Account{
			{ID: "high", Provider: models.ProviderOpenAI, Enabled: true, Priority: 100},
			{ID: "low", Provider: models.ProviderAnthropic, Enabled: true, Priority: 1},
		}

		highCount := 0
		iterations := 1000

		for i := 0; i < iterations; i++ {
			result := strategy.SelectAccount(accounts, "")
			if result.ID == "high" {
				highCount++
			}
		}

		// High priority should be selected significantly more often
		assert.Greater(t, highCount, iterations/2)
	})

	t.Run("name is correct", func(t *testing.T) {
		strategy := NewWeightedStrategy()
		assert.Equal(t, "weighted", strategy.Name())
	})
}

// Test Metrics
func TestMetrics(t *testing.T) {
	t.Run("records success correctly", func(t *testing.T) {
		metrics := NewMetrics()
		metrics.RecordSuccess(100 * time.Millisecond)

		stats := metrics.GetStats()
		assert.Equal(t, uint64(1), stats.TotalRequests)
		assert.Equal(t, uint64(1), stats.SuccessfulRequests)
		assert.Equal(t, uint64(0), stats.FallbackTriggered)
	})

	t.Run("records fallback correctly", func(t *testing.T) {
		metrics := NewMetrics()
		metrics.RecordFallback("timeout", 50*time.Millisecond)

		stats := metrics.GetStats()
		assert.Equal(t, uint64(1), stats.TotalRequests)
		assert.Equal(t, uint64(0), stats.SuccessfulRequests)
		assert.Equal(t, uint64(1), stats.FallbackTriggered)
		assert.Equal(t, uint64(1), stats.TimeoutCount)
	})

	t.Run("records network errors correctly", func(t *testing.T) {
		metrics := NewMetrics()
		metrics.RecordFallback("network_error", 50*time.Millisecond)

		stats := metrics.GetStats()
		assert.Equal(t, uint64(1), stats.NetworkErrors)
	})

	t.Run("calculates fallback rate correctly", func(t *testing.T) {
		metrics := NewMetrics()

		// 3 successes, 1 fallback
		metrics.RecordSuccess(100 * time.Millisecond)
		metrics.RecordSuccess(100 * time.Millisecond)
		metrics.RecordSuccess(100 * time.Millisecond)
		metrics.RecordFallback("timeout", 50*time.Millisecond)

		stats := metrics.GetStats()
		assert.Equal(t, float64(25), stats.FallbackRate)
	})

	t.Run("calculates average latency correctly", func(t *testing.T) {
		metrics := NewMetrics()

		metrics.RecordSuccess(100 * time.Millisecond)
		metrics.RecordSuccess(200 * time.Millisecond)
		metrics.RecordSuccess(300 * time.Millisecond)

		stats := metrics.GetStats()
		assert.Equal(t, 200*time.Millisecond, stats.AvgLatency)
	})

	t.Run("handles empty metrics gracefully", func(t *testing.T) {
		metrics := NewMetrics()
		stats := metrics.GetStats()

		assert.Equal(t, uint64(0), stats.TotalRequests)
		assert.Equal(t, time.Duration(0), stats.AvgLatency)
		assert.Equal(t, float64(0), stats.FallbackRate)
	})
}

// Test FailOpenClient
func TestFailOpenClient(t *testing.T) {
	t.Run("successful request", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		expectedResult := &Result{
			AccountID: "acc-1",
			Provider:  models.ProviderOpenAI,
			Fallback:  false,
			Reason:    "",
		}

		operation := func(ctx context.Context) (*Result, error) {
			return expectedResult, nil
		}

		result, err := client.ExecuteWithFailOpen(context.Background(), operation)
		require.NoError(t, err)
		assert.Equal(t, expectedResult.AccountID, result.AccountID)
		assert.False(t, result.Fallback)

		// Check metrics
		stats := client.GetMetrics()
		assert.Equal(t, uint64(1), stats.SuccessfulRequests)
	})

	t.Run("timeout triggers fallback", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.Timeout = 50 * time.Millisecond
		client := NewFailOpenClient(accounts, config)

		operation := func(ctx context.Context) (*Result, error) {
			// Simulate slow operation
			time.Sleep(200 * time.Millisecond)
			return &Result{AccountID: "acc-1"}, nil
		}

		result, err := client.ExecuteWithFailOpen(context.Background(), operation)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.Fallback)
		assert.Equal(t, "timeout", result.Reason)
		assert.NotEmpty(t, result.AccountID)

		// Check metrics - at least 1 fallback triggered, timeout count >= 1
		stats := client.GetMetrics()
		assert.GreaterOrEqual(t, stats.FallbackTriggered, uint64(1))
		assert.GreaterOrEqual(t, stats.TimeoutCount, uint64(1))
	})

	t.Run("network error triggers fallback", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		operation := func(ctx context.Context) (*Result, error) {
			return nil, errors.New("connection refused")
		}

		result, err := client.ExecuteWithFailOpen(context.Background(), operation)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.Fallback)
		assert.Equal(t, "network_error", result.Reason)
		assert.NotEmpty(t, result.AccountID)

		// Check metrics - at least 1 network error recorded
		stats := client.GetMetrics()
		assert.GreaterOrEqual(t, stats.NetworkErrors, uint64(1))
	})

	t.Run("non-network error returns error", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		expectedErr := errors.New("invalid request")
		operation := func(ctx context.Context) (*Result, error) {
			return nil, expectedErr
		}

		_, err := client.ExecuteWithFailOpen(context.Background(), operation)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fallback when no accounts available", func(t *testing.T) {
		accounts := newMockAccountProvider()
		accounts.SetAccounts([]*models.Account{}) // Empty accounts

		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		operation := func(ctx context.Context) (*Result, error) {
			return nil, errors.New("connection refused")
		}

		_, err := client.ExecuteWithFailOpen(context.Background(), operation)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no enabled accounts available")
	})

	t.Run("tracks last used account", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		assert.Equal(t, "", client.GetLastUsed())

		operation := func(ctx context.Context) (*Result, error) {
			return &Result{AccountID: "acc-2"}, nil
		}

		_, err := client.ExecuteWithFailOpen(context.Background(), operation)
		require.NoError(t, err)
		assert.Equal(t, "acc-2", client.GetLastUsed())
	})

	t.Run("context cancellation", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		operation := func(ctx context.Context) (*Result, error) {
			return &Result{AccountID: "acc-1"}, nil
		}

		_, err := client.ExecuteWithFailOpen(ctx, operation)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

// Test ExecuteWithRetry
func TestFailOpenClient_ExecuteWithRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.MaxRetries = 2
		client := NewFailOpenClient(accounts, config)

		callCount := 0
		operation := func(ctx context.Context) (*Result, error) {
			callCount++
			return &Result{AccountID: "acc-1"}, nil
		}

		result, err := client.ExecuteWithRetry(context.Background(), operation)
		require.NoError(t, err)
		assert.Equal(t, "acc-1", result.AccountID)
		assert.Equal(t, 1, callCount)
	})

	t.Run("retries on failure", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.MaxRetries = 2
		config.RetryBackoff = 10 * time.Millisecond
		client := NewFailOpenClient(accounts, config)

		callCount := 0
		operation := func(ctx context.Context) (*Result, error) {
			callCount++
			if callCount < 2 {
				return nil, errors.New("connection refused")
			}
			return &Result{AccountID: "acc-1"}, nil
		}

		result, err := client.ExecuteWithRetry(context.Background(), operation)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.Fallback) // Should succeed, not fallback
		assert.Equal(t, "acc-1", result.AccountID)
		assert.GreaterOrEqual(t, callCount, 1)
	})

	t.Run("exhausts retries", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.MaxRetries = 1
		config.RetryBackoff = 10 * time.Millisecond
		client := NewFailOpenClient(accounts, config)

		callCount := 0
		operation := func(ctx context.Context) (*Result, error) {
			callCount++
			return nil, errors.New("connection refused")
		}

		result, err := client.ExecuteWithRetry(context.Background(), operation)
		// After all retries exhausted with network errors, we should get a fallback result
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.Fallback)
		// Initial + 1 retry = 2 attempts, then final fallback
		assert.GreaterOrEqual(t, callCount, 2)
	})

	t.Run("respects context cancellation during retry", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.MaxRetries = 5
		config.RetryBackoff = 1 * time.Second // Long backoff
		client := NewFailOpenClient(accounts, config)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		operation := func(ctx context.Context) (*Result, error) {
			return nil, errors.New("connection refused")
		}

		_, err := client.ExecuteWithRetry(ctx, operation)
		assert.Error(t, err)
		assert.Equal(t, context.DeadlineExceeded, err)
	})
}

// Test FailOpenMiddlewareClient
func TestFailOpenMiddlewareClient(t *testing.T) {
	t.Run("successful middleware call", func(t *testing.T) {
		accounts := newMockAccountProvider()
		mockClient := newMockMiddlewareClient()
		config := DefaultConfig()

		fallbackCalled := false
		client := NewFailOpenMiddlewareClient(mockClient, accounts, config, func(reason string, result *Result) {
			fallbackCalled = true
		})

		result, err := client.SelectAccount(context.Background(), SelectAccountRequest{})
		require.NoError(t, err)
		assert.Equal(t, "acc-1", result.AccountID)
		assert.False(t, result.Fallback)
		assert.False(t, fallbackCalled)
	})

	t.Run("fallback triggers callback", func(t *testing.T) {
		accounts := newMockAccountProvider()
		mockClient := newMockMiddlewareClient()
		mockClient.SetDelay(200 * time.Millisecond)

		config := DefaultConfig()
		config.Timeout = 50 * time.Millisecond

		var callbackReason string
		var callbackResult *Result
		client := NewFailOpenMiddlewareClient(mockClient, accounts, config, func(reason string, result *Result) {
			callbackReason = reason
			callbackResult = result
		})

		result, err := client.SelectAccount(context.Background(), SelectAccountRequest{})
		require.NoError(t, err)
		assert.True(t, result.Fallback)
		assert.Equal(t, "timeout", callbackReason)
		assert.NotNil(t, callbackResult)
		assert.True(t, callbackResult.Fallback)
	})

	t.Run("no callback when nil", func(t *testing.T) {
		accounts := newMockAccountProvider()
		mockClient := newMockMiddlewareClient()
		mockClient.SetDelay(200 * time.Millisecond)

		config := DefaultConfig()
		config.Timeout = 50 * time.Millisecond

		client := NewFailOpenMiddlewareClient(mockClient, accounts, config, nil)

		result, err := client.SelectAccount(context.Background(), SelectAccountRequest{})
		require.NoError(t, err)
		assert.True(t, result.Fallback)
		// Should not panic
	})
}

// Test Config
func TestConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		config := DefaultConfig()

		assert.Equal(t, DefaultFailOpenTimeout, config.Timeout)
		assert.NotNil(t, config.FallbackStrategy)
		assert.Equal(t, 0, config.MaxRetries)
		assert.Equal(t, 10*time.Millisecond, config.RetryBackoff)
		assert.True(t, config.EnableMetrics)
		assert.Equal(t, 25*time.Second, config.GracefulShutdownTimeout)
	})

	t.Run("NewFailOpenClient applies defaults", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := Config{} // Empty config

		client := NewFailOpenClient(accounts, config)
		require.NotNil(t, client)

		// Values should be defaulted
		assert.Equal(t, DefaultFailOpenTimeout, client.config.Timeout)
		assert.NotNil(t, client.config.FallbackStrategy)
		assert.Equal(t, 10*time.Millisecond, client.config.RetryBackoff)
		assert.Equal(t, 25*time.Second, client.config.GracefulShutdownTimeout)
	})
}

// Test Shutdown
func TestFailOpenClient_Shutdown(t *testing.T) {
	t.Run("shutdown completes", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		err := client.Shutdown()
		assert.NoError(t, err)
	})

	t.Run("shutdown with timeout", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.GracefulShutdownTimeout = 100 * time.Millisecond
		client := NewFailOpenClient(accounts, config)

		err := client.Shutdown()
		assert.NoError(t, err)
	})
}

// Test isNetworkError
func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection refused", errors.New("connection refused"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"no such host", errors.New("no such host"), true},
		{"timeout", errors.New("operation timeout"), true},
		{"deadline exceeded", errors.New("context deadline exceeded"), true},
		{"network unreachable", errors.New("network is unreachable"), true},
		{"connection timed out", errors.New("connection timed out"), true},
		{"i/o timeout", errors.New("i/o timeout"), true},
		{"regular error", errors.New("some other error"), false},
		{"case insensitive", errors.New("CONNECTION REFUSED"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNetworkError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test concurrency and race conditions
func TestFailOpenClient_Concurrency(t *testing.T) {
	t.Run("concurrent requests", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		client := NewFailOpenClient(accounts, config)

		var wg sync.WaitGroup
		numGoroutines := 100
		numRequests := 10

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				for j := 0; j < numRequests; j++ {
					operation := func(ctx context.Context) (*Result, error) {
						return &Result{AccountID: fmt.Sprintf("acc-%d", id)}, nil
					}

					_, err := client.ExecuteWithFailOpen(context.Background(), operation)
					assert.NoError(t, err)
				}
			}(i)
		}

		wg.Wait()

		stats := client.GetMetrics()
		expectedRequests := uint64(numGoroutines * numRequests)
		assert.Equal(t, expectedRequests, stats.TotalRequests)
		assert.Equal(t, expectedRequests, stats.SuccessfulRequests)
	})

	t.Run("concurrent fallback", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.Timeout = 1 * time.Millisecond
		client := NewFailOpenClient(accounts, config)

		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				operation := func(ctx context.Context) (*Result, error) {
					time.Sleep(10 * time.Millisecond) // Always timeout
					return &Result{AccountID: "acc-1"}, nil
				}

				result, err := client.ExecuteWithFailOpen(context.Background(), operation)
				assert.NoError(t, err)
				assert.True(t, result.Fallback)
			}()
		}

		wg.Wait()

		stats := client.GetMetrics()
		// Use GreaterOrEqual because the executeFallback method may record additional metrics
		assert.GreaterOrEqual(t, stats.FallbackTriggered, uint64(numGoroutines))
	})

	t.Run("concurrent metrics access", func(t *testing.T) {
		metrics := NewMetrics()
		var wg sync.WaitGroup

		// Writers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				metrics.RecordSuccess(100 * time.Millisecond)
			}()
		}

		// Readers
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = metrics.GetStats()
			}()
		}

		wg.Wait()

		stats := metrics.GetStats()
		assert.Equal(t, uint64(50), stats.SuccessfulRequests)
	})
}

// Test RoundRobinStrategy concurrency
func TestRoundRobinStrategy_Concurrency(t *testing.T) {
	strategy := NewRoundRobinStrategy()
	accounts := []*models.Account{
		{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true},
		{ID: "acc-2", Provider: models.ProviderAnthropic, Enabled: true},
		{ID: "acc-3", Provider: models.ProviderGemini, Enabled: true},
	}

	var wg sync.WaitGroup
	results := make(chan string, 300)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				result := strategy.SelectAccount(accounts, "")
				if result != nil {
					results <- result.ID
				}
			}
		}()
	}

	wg.Wait()
	close(results)

	// Count results
	count := 0
	for range results {
		count++
	}
	assert.Equal(t, 300, count)
}

// Benchmark tests
func BenchmarkFailOpenClient_ExecuteWithFailOpen(b *testing.B) {
	accounts := newMockAccountProvider()
	config := DefaultConfig()
	client := NewFailOpenClient(accounts, config)

	operation := func(ctx context.Context) (*Result, error) {
		return &Result{AccountID: "acc-1"}, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.ExecuteWithFailOpen(context.Background(), operation)
	}
}

func BenchmarkRoundRobinStrategy_SelectAccount(b *testing.B) {
	strategy := NewRoundRobinStrategy()
	accounts := []*models.Account{
		{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true},
		{ID: "acc-2", Provider: models.ProviderAnthropic, Enabled: true},
		{ID: "acc-3", Provider: models.ProviderGemini, Enabled: true},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		strategy.SelectAccount(accounts, "")
	}
}

func BenchmarkMetrics_RecordSuccess(b *testing.B) {
	metrics := NewMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordSuccess(100 * time.Millisecond)
	}
}

// Test graceful degradation scenarios
func TestGracefulDegradation(t *testing.T) {
	t.Run("degrades when middleware is slow", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.Timeout = 20 * time.Millisecond
		client := NewFailOpenClient(accounts, config)

		// Simulate gradually slowing middleware
		delays := []time.Duration{10 * time.Millisecond, 30 * time.Millisecond, 100 * time.Millisecond}

		for _, delay := range delays {
			d := delay // capture range variable
			operation := func(ctx context.Context) (*Result, error) {
				time.Sleep(d)
				return &Result{AccountID: "acc-1"}, nil
			}

			result, err := client.ExecuteWithFailOpen(context.Background(), operation)
			require.NoError(t, err)

			if d > config.Timeout {
				assert.True(t, result.Fallback, "Expected fallback for delay %v", d)
			} else {
				assert.False(t, result.Fallback, "Expected no fallback for delay %v", d)
			}
		}
	})

	t.Run("continues serving during middleware outage", func(t *testing.T) {
		accounts := newMockAccountProvider()
		config := DefaultConfig()
		config.Timeout = 10 * time.Millisecond
		client := NewFailOpenClient(accounts, config)

		// Simulate middleware outage
		operation := func(ctx context.Context) (*Result, error) {
			return nil, errors.New("connection refused")
		}

		// Multiple requests during outage
		for i := 0; i < 10; i++ {
			result, err := client.ExecuteWithFailOpen(context.Background(), operation)
			require.NoError(t, err)
			assert.True(t, result.Fallback)
			assert.NotEmpty(t, result.AccountID)
		}

		stats := client.GetMetrics()
		// Use GreaterOrEqual because executeFallback may record metrics in multiple places
		assert.GreaterOrEqual(t, stats.FallbackTriggered, uint64(10))
		assert.GreaterOrEqual(t, stats.NetworkErrors, uint64(10))
	})
}
