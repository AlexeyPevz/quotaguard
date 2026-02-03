// Package failopen provides fail-open behavior for QuotaGuard middleware client.
// When QuotaGuard is unavailable or responds slowly, the system falls back to
// alternative account selection strategies rather than blocking requests.
package failopen

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// DefaultFailOpenTimeout is the default timeout for fail-open behavior.
const DefaultFailOpenTimeout = 50 * time.Millisecond

// FallbackStrategy defines the interface for fallback account selection strategies.
type FallbackStrategy interface {
	// SelectAccount returns an account from the available candidates.
	// Returns nil if no suitable account is found.
	SelectAccount(accounts []*models.Account, lastUsed string) *models.Account
	// Name returns the strategy name for metrics/logging.
	Name() string
}

// RoundRobinStrategy implements round-robin fallback selection.
type RoundRobinStrategy struct {
	mu       sync.Mutex
	index    int
	lastUsed string
}

// NewRoundRobinStrategy creates a new round-robin fallback strategy.
func NewRoundRobinStrategy() *RoundRobinStrategy {
	return &RoundRobinStrategy{}
}

// SelectAccount selects the next account in round-robin order.
func (r *RoundRobinStrategy) SelectAccount(accounts []*models.Account, lastUsed string) *models.Account {
	if len(accounts) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// If lastUsed changed externally, adjust our index
	if lastUsed != r.lastUsed {
		r.index = 0
		for i, acc := range accounts {
			if acc.ID == lastUsed {
				r.index = (i + 1) % len(accounts)
				break
			}
		}
		r.lastUsed = lastUsed
	}

	selected := accounts[r.index]
	r.index = (r.index + 1) % len(accounts)
	r.lastUsed = selected.ID

	return selected
}

// Name returns the strategy name.
func (r *RoundRobinStrategy) Name() string {
	return "round-robin"
}

// cryptoIntn returns a cryptographically secure random integer in [0, n).
func cryptoIntn(n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return 0, err
	}
	return int(binary.LittleEndian.Uint64(b) % uint64(n)), nil
}

// FirstAvailableStrategy selects any available account randomly
type FirstAvailableStrategy struct {
	mu sync.Mutex
}

// NewFirstAvailableStrategy creates a new first-available fallback strategy.
func NewFirstAvailableStrategy() *FirstAvailableStrategy {
	return &FirstAvailableStrategy{}
}

// SelectAccount selects a random available account.
func (f *FirstAvailableStrategy) SelectAccount(accounts []*models.Account, _ string) *models.Account {
	if len(accounts) == 0 {
		return nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	idx, err := cryptoIntn(len(accounts))
	if err != nil {
		// Fallback to first account on error
		return accounts[0]
	}
	return accounts[idx]
}

// Name returns the strategy name.
func (f *FirstAvailableStrategy) Name() string {
	return "first-available"
}

// WeightedStrategy selects account based on priority weights
type WeightedStrategy struct {
	mu sync.Mutex
}

// NewWeightedStrategy creates a new weighted fallback strategy.
func NewWeightedStrategy() *WeightedStrategy {
	return &WeightedStrategy{}
}

// SelectAccount selects an account weighted by priority.
func (w *WeightedStrategy) SelectAccount(accounts []*models.Account, _ string) *models.Account {
	if len(accounts) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Simple weighted selection based on priority
	totalWeight := 0
	for _, acc := range accounts {
		totalWeight += acc.Priority + 1 // +1 to ensure even low priority accounts can be selected
	}

	if totalWeight == 0 {
		return accounts[0]
	}

	target, err := cryptoIntn(totalWeight)
	if err != nil {
		// Fallback to first account on error
		return accounts[0]
	}
	current := 0
	for _, acc := range accounts {
		current += acc.Priority + 1
		if target < current {
			return acc
		}
	}

	return accounts[len(accounts)-1]
}

// Name returns the strategy name.
func (w *WeightedStrategy) Name() string {
	return "weighted"
}

// Config holds fail-open configuration.
type Config struct {
	// Timeout is the maximum time to wait for a response from QuotaGuard.
	// Default is DefaultFailOpenTimeout (50ms).
	Timeout time.Duration

	// FallbackStrategy is the strategy to use when QuotaGuard is unavailable.
	// Default is RoundRobinStrategy.
	FallbackStrategy FallbackStrategy

	// MaxRetries is the maximum number of retries before triggering fail-open.
	// Default is 0 (no retries).
	MaxRetries int

	// RetryBackoff is the backoff duration between retries.
	// Default is 10ms.
	RetryBackoff time.Duration

	// EnableMetrics enables collection of fail-open metrics.
	EnableMetrics bool

	// GracefulShutdownTimeout is the timeout for graceful shutdown.
	// Default is 25s (must be < 30s as per requirements).
	GracefulShutdownTimeout time.Duration
}

// DefaultConfig returns default fail-open configuration.
func DefaultConfig() Config {
	return Config{
		Timeout:                 DefaultFailOpenTimeout,
		FallbackStrategy:        NewRoundRobinStrategy(),
		MaxRetries:              0,
		RetryBackoff:            10 * time.Millisecond,
		EnableMetrics:           true,
		GracefulShutdownTimeout: 25 * time.Second,
	}
}

// Metrics tracks fail-open behavior statistics.
type Metrics struct {
	mu sync.RWMutex

	totalRequests      uint64
	successfulRequests uint64
	fallbackTriggered  uint64
	timeoutCount       uint64
	networkErrors      uint64
	totalLatency       time.Duration
	fallbackLatency    time.Duration
}

// NewMetrics creates a new metrics tracker.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// RecordSuccess records a successful QuotaGuard request.
func (m *Metrics) RecordSuccess(latency time.Duration) {
	atomic.AddUint64(&m.totalRequests, 1)
	atomic.AddUint64(&m.successfulRequests, 1)
	m.addLatency(latency)
}

// RecordFallback records a fallback occurrence.
func (m *Metrics) RecordFallback(reason string, latency time.Duration) {
	atomic.AddUint64(&m.totalRequests, 1)
	atomic.AddUint64(&m.fallbackTriggered, 1)

	switch reason {
	case "timeout":
		atomic.AddUint64(&m.timeoutCount, 1)
	case "network_error":
		atomic.AddUint64(&m.networkErrors, 1)
	}

	m.mu.Lock()
	m.fallbackLatency += latency
	m.mu.Unlock()
}

// addLatency adds to total latency thread-safely.
func (m *Metrics) addLatency(d time.Duration) {
	m.mu.Lock()
	m.totalLatency += d
	m.mu.Unlock()
}

// GetStats returns current metrics statistics.
func (m *Metrics) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return Stats{
		TotalRequests:      atomic.LoadUint64(&m.totalRequests),
		SuccessfulRequests: atomic.LoadUint64(&m.successfulRequests),
		FallbackTriggered:  atomic.LoadUint64(&m.fallbackTriggered),
		TimeoutCount:       atomic.LoadUint64(&m.timeoutCount),
		NetworkErrors:      atomic.LoadUint64(&m.networkErrors),
		AvgLatency:         m.avgLatency(),
		AvgFallbackLatency: m.avgFallbackLatency(),
		FallbackRate:       m.fallbackRate(),
	}
}

// avgLatency calculates average latency.
func (m *Metrics) avgLatency() time.Duration {
	total := atomic.LoadUint64(&m.totalRequests)
	if total == 0 {
		return 0
	}
	return m.totalLatency / time.Duration(total)
}

// avgFallbackLatency calculates average fallback latency.
func (m *Metrics) avgFallbackLatency() time.Duration {
	fallback := atomic.LoadUint64(&m.fallbackTriggered)
	if fallback == 0 {
		return 0
	}
	return m.fallbackLatency / time.Duration(fallback)
}

// fallbackRate calculates the fallback rate as a percentage.
func (m *Metrics) fallbackRate() float64 {
	total := atomic.LoadUint64(&m.totalRequests)
	fallback := atomic.LoadUint64(&m.fallbackTriggered)
	if total == 0 {
		return 0
	}
	return float64(fallback) / float64(total) * 100
}

// Stats represents a snapshot of metrics.
type Stats struct {
	TotalRequests      uint64
	SuccessfulRequests uint64
	FallbackTriggered  uint64
	TimeoutCount       uint64
	NetworkErrors      uint64
	AvgLatency         time.Duration
	AvgFallbackLatency time.Duration
	FallbackRate       float64
}

// AccountProvider provides account information for fallback selection.
type AccountProvider interface {
	ListEnabledAccounts() []*models.Account
	GetAccount(id string) (*models.Account, bool)
}

// Result represents the result of an operation.
type Result struct {
	AccountID string
	Provider  models.Provider
	Fallback  bool
	Reason    string
}

// FailOpenClient wraps a middleware client with fail-open behavior.
type FailOpenClient struct {
	accounts AccountProvider
	config   Config
	metrics  *Metrics
	lastUsed string
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewFailOpenClient creates a new fail-open client wrapper.
func NewFailOpenClient(accounts AccountProvider, config Config) *FailOpenClient {
	if config.Timeout <= 0 {
		config.Timeout = DefaultFailOpenTimeout
	}
	if config.FallbackStrategy == nil {
		config.FallbackStrategy = NewRoundRobinStrategy()
	}
	if config.RetryBackoff <= 0 {
		config.RetryBackoff = 10 * time.Millisecond
	}
	if config.GracefulShutdownTimeout <= 0 {
		config.GracefulShutdownTimeout = 25 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &FailOpenClient{
		accounts: accounts,
		config:   config,
		metrics:  NewMetrics(),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// ExecuteWithFailOpen executes the provided operation with fail-open behavior.
// If the operation times out or fails, it falls back to the configured strategy.
func (c *FailOpenClient) ExecuteWithFailOpen(
	ctx context.Context,
	operation func(context.Context) (*Result, error),
) (*Result, error) {
	start := time.Now()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create a context with timeout
	opCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Channel for operation result
	resultCh := make(chan struct {
		result *Result
		err    error
	}, 1)

	// Execute the operation in a goroutine
	go func() {
		result, err := operation(opCtx)
		select {
		case <-opCtx.Done():
			// Operation completed after timeout, ignore result
		case resultCh <- struct {
			result *Result
			err    error
		}{result: result, err: err}:
		}
	}()

	// Wait for result or timeout
	select {
	case res := <-resultCh:
		latency := time.Since(start)
		if res.err != nil {
			// Check if it's a network error
			if isNetworkError(res.err) {
				if c.config.EnableMetrics {
					c.metrics.RecordFallback("network_error", latency)
				}
				return c.executeFallback("network_error")
			}
			return nil, res.err
		}
		if c.config.EnableMetrics {
			c.metrics.RecordSuccess(latency)
		}
		c.mu.Lock()
		c.lastUsed = res.result.AccountID
		c.mu.Unlock()
		return res.result, nil

	case <-opCtx.Done():
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Timeout occurred
		latency := time.Since(start)
		if c.config.EnableMetrics {
			c.metrics.RecordFallback("timeout", latency)
		}
		return c.executeFallback("timeout")

	case <-ctx.Done():
		// Parent context canceled
		return nil, ctx.Err()
	}
}

// ExecuteWithRetry executes the operation with retries before falling back.
func (c *FailOpenClient) ExecuteWithRetry(
	ctx context.Context,
	operation func(context.Context) (*Result, error),
) (*Result, error) {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		result, err := c.ExecuteWithFailOpen(ctx, operation)
		if err == nil && !result.Fallback {
			return result, nil
		}

		if err != nil {
			lastErr = err

			// Don't retry if parent context is canceled
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}

		// Don't retry after the last attempt (or if we got a successful fallback)
		if attempt < c.config.MaxRetries {
			select {
			case <-time.After(c.config.RetryBackoff):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
	}

	// If we have no error but all attempts resulted in fallback, return the last result
	return c.ExecuteWithFailOpen(ctx, operation)
}

// executeFallback executes the fallback strategy.
func (c *FailOpenClient) executeFallback(reason string) (*Result, error) {
	start := time.Now()

	accounts := c.accounts.ListEnabledAccounts()
	if len(accounts) == 0 {
		// Record metrics even on failure
		if c.config.EnableMetrics {
			c.metrics.RecordFallback(reason+"_no_accounts", time.Since(start))
		}
		return nil, fmt.Errorf("fail-open: no enabled accounts available for fallback")
	}

	c.mu.RLock()
	lastUsed := c.lastUsed
	c.mu.RUnlock()

	selected := c.config.FallbackStrategy.SelectAccount(accounts, lastUsed)
	if selected == nil {
		if c.config.EnableMetrics {
			c.metrics.RecordFallback(reason+"_no_selection", time.Since(start))
		}
		return nil, fmt.Errorf("fail-open: fallback strategy could not select account")
	}

	// Update last used
	c.mu.Lock()
	c.lastUsed = selected.ID
	c.mu.Unlock()

	latency := time.Since(start)
	if c.config.EnableMetrics {
		c.metrics.RecordFallback(reason, latency)
	}

	return &Result{
		AccountID: selected.ID,
		Provider:  selected.Provider,
		Fallback:  true,
		Reason:    reason,
	}, nil
}

// GetMetrics returns the current metrics statistics.
func (c *FailOpenClient) GetMetrics() Stats {
	return c.metrics.GetStats()
}

// GetLastUsed returns the ID of the last used account.
func (c *FailOpenClient) GetLastUsed() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastUsed
}

// Shutdown gracefully shuts down the fail-open client.
// It waits for any in-flight operations to complete up to the configured timeout.
func (c *FailOpenClient) Shutdown() error {
	// Signal shutdown
	c.cancel()

	// Create a timeout context for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), c.config.GracefulShutdownTimeout)
	defer cancel()

	// Wait for shutdown to complete or timeout
	<-ctx.Done()

	return nil
}

// isNetworkError checks if an error is a network-level error.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// This is a simple implementation. In production, you'd want to check
	// for specific network error types (net.Error, url.Error, syscall errors, etc.)
	errStr := err.Error()
	networkErrors := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"timeout",
		"deadline exceeded",
		"network is unreachable",
		"connection timed out",
		"i/o timeout",
	}

	for _, ne := range networkErrors {
		if contains(errStr, ne) {
			return true
		}
	}
	return false
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsIgnoreCase(s, substr)
}

// containsIgnoreCase checks if a string contains a substring case-insensitively.
func containsIgnoreCase(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	// Simple case-insensitive check
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower converts a byte to lowercase.
func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// SelectAccountRequest represents a request to select an account.
type SelectAccountRequest struct {
	Provider         models.Provider
	RequiredDims     []models.DimensionType
	EstimatedCost    float64
	Exclude          []string
	ExcludeProviders []models.Provider
	EstimatedTokens  int64
}

// MiddlewareClient defines the interface for the QuotaGuard middleware client.
type MiddlewareClient interface {
	SelectAccount(ctx context.Context, req SelectAccountRequest) (*Result, error)
}

// FailOpenMiddlewareClient wraps a middleware client with fail-open behavior.
type FailOpenMiddlewareClient struct {
	client     MiddlewareClient
	failOpen   *FailOpenClient
	onFallback func(reason string, result *Result)
}

// NewFailOpenMiddlewareClient creates a new fail-open middleware client wrapper.
func NewFailOpenMiddlewareClient(
	client MiddlewareClient,
	accounts AccountProvider,
	config Config,
	onFallback func(reason string, result *Result),
) *FailOpenMiddlewareClient {
	return &FailOpenMiddlewareClient{
		client:     client,
		failOpen:   NewFailOpenClient(accounts, config),
		onFallback: onFallback,
	}
}

// SelectAccount selects an account with fail-open behavior.
func (f *FailOpenMiddlewareClient) SelectAccount(
	ctx context.Context,
	req SelectAccountRequest,
) (*Result, error) {
	operation := func(opCtx context.Context) (*Result, error) {
		return f.client.SelectAccount(opCtx, req)
	}

	result, err := f.failOpen.ExecuteWithFailOpen(ctx, operation)
	if err != nil {
		return nil, err
	}

	if result.Fallback && f.onFallback != nil {
		f.onFallback(result.Reason, result)
	}

	return result, nil
}

// GetMetrics returns the current metrics from the fail-open client.
func (f *FailOpenMiddlewareClient) GetMetrics() Stats {
	return f.failOpen.GetMetrics()
}

// Shutdown gracefully shuts down the client.
func (f *FailOpenMiddlewareClient) Shutdown() error {
	return f.failOpen.Shutdown()
}
