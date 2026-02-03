package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/errors"
	"github.com/quotaguard/quotaguard/internal/metrics"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// QuotaFetcher defines the interface for fetching quota from providers
type QuotaFetcher interface {
	FetchQuota(ctx context.Context, accountID string) (*models.QuotaInfo, error)
}

// ActiveCollector actively polls providers for quota information
type ActiveCollector struct {
	store         store.Store
	fetcher       QuotaFetcher
	interval      time.Duration
	adaptive      bool
	timeout       time.Duration
	retryAttempts int
	retryBackoff  time.Duration

	// Metrics
	metrics *metrics.Metrics

	// Circuit breaker
	cb        *CircuitBreaker
	cbEnabled bool

	// Adaptive interval control
	mu              sync.RWMutex
	currentInterval time.Duration
	lastQuotaPct    float64

	// Control
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// CircuitBreaker implements a simple circuit breaker pattern
type CircuitBreaker struct {
	mu               sync.RWMutex
	failures         int
	failureThreshold int
	timeout          time.Duration
	lastFailureTime  time.Time
	state            CircuitState
	metrics          *metrics.Metrics
}

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	// CircuitClosed means the circuit is closed and requests are allowed
	CircuitClosed CircuitState = iota
	// CircuitOpen means the circuit is open and requests are blocked
	CircuitOpen
	// CircuitHalfOpen means the circuit is testing if the service recovered
	CircuitHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(failureThreshold int, timeout time.Duration, m *metrics.Metrics) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		timeout:          timeout,
		state:            CircuitClosed,
		metrics:          m,
	}
}

// Allow checks if a request should be allowed
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = CircuitHalfOpen
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	oldState := cb.state
	cb.state = CircuitClosed

	if oldState != CircuitClosed && cb.metrics != nil {
		cb.metrics.RecordCollector("circuit_breaker", "closed", "active")
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.failures >= cb.failureThreshold {
		oldState := cb.state
		cb.state = CircuitOpen
		if oldState != CircuitOpen && cb.metrics != nil {
			cb.metrics.RecordCollector("circuit_breaker", "opened", "active")
		}
	}
}

// State returns the current circuit state
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Config holds configuration for the active collector
type Config struct {
	Interval      time.Duration
	Adaptive      bool
	Timeout       time.Duration
	RetryAttempts int
	RetryBackoff  time.Duration
	CBEnabled     bool
	CBThreshold   int
	CBTimeout     time.Duration
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Interval:      60 * time.Second,
		Adaptive:      true,
		Timeout:       10 * time.Second,
		RetryAttempts: 3,
		RetryBackoff:  time.Second,
		CBEnabled:     true,
		CBThreshold:   3,
		CBTimeout:     5 * time.Minute,
	}
}

// NewActiveCollector creates a new active collector
func NewActiveCollector(s store.Store, fetcher QuotaFetcher, cfg Config, m *metrics.Metrics) *ActiveCollector {
	ac := &ActiveCollector{
		store:           s,
		fetcher:         fetcher,
		interval:        cfg.Interval,
		adaptive:        cfg.Adaptive,
		timeout:         cfg.Timeout,
		retryAttempts:   cfg.RetryAttempts,
		retryBackoff:    cfg.RetryBackoff,
		cbEnabled:       cfg.CBEnabled,
		currentInterval: cfg.Interval,
		metrics:         m,
	}

	if cfg.CBEnabled {
		ac.cb = NewCircuitBreaker(cfg.CBThreshold, cfg.CBTimeout, m)
	}

	return ac
}

// Start begins the collector's polling loop
func (ac *ActiveCollector) Start(ctx context.Context) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.running {
		return &errors.ErrServerStart{Addr: "active-collector", Err: fmt.Errorf("collector already running")}
	}

	ac.running = true
	ac.stopCh = make(chan struct{})
	ac.wg.Add(1)
	go ac.pollLoop(ctx)

	return nil
}

// Stop gracefully shuts down the collector
func (ac *ActiveCollector) Stop() error {
	ac.mu.Lock()
	if !ac.running {
		ac.mu.Unlock()
		return nil
	}
	ac.running = false
	stopCh := ac.stopCh
	ac.mu.Unlock()

	close(stopCh)
	ac.wg.Wait()

	return nil
}

// IsRunning returns true if the collector is running
func (ac *ActiveCollector) IsRunning() bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.running
}

// pollLoop is the main polling loop
func (ac *ActiveCollector) pollLoop(ctx context.Context) {
	defer ac.wg.Done()

	// Do initial poll
	ac.poll(ctx)

	ticker := time.NewTicker(ac.getInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ac.stopCh:
			return
		case <-ticker.C:
			ac.poll(ctx)
			// Update ticker if interval changed
			if ac.adaptive {
				ticker.Reset(ac.getInterval())
			}
		}
	}
}

// poll fetches quota for all enabled accounts
func (ac *ActiveCollector) poll(ctx context.Context) {
	if ac.cbEnabled && !ac.cb.Allow() {
		return
	}

	accounts := ac.store.ListEnabledAccounts()
	if len(accounts) == 0 {
		return
	}

	successCount := 0
	failCount := 0

	for _, acc := range accounts {
		quota, err := ac.fetchWithRetry(ctx, acc.ID)
		if err != nil {
			failCount++
			if ac.metrics != nil {
				ac.metrics.RecordCollector("fetch", "failure", "active")
			}
			continue
		}

		successCount++
		ac.store.SetQuota(acc.ID, quota)
		if ac.metrics != nil {
			ac.metrics.RecordCollector("fetch", "success", "active")
			ac.metrics.RecordQuotaUtilization(acc.ID, string(acc.Provider), "all", quota.EffectiveRemainingPct)
		}
	}

	// Update circuit breaker
	if ac.cbEnabled {
		if failCount > 0 && successCount == 0 {
			ac.cb.RecordFailure()
		} else if successCount > 0 {
			ac.cb.RecordSuccess()
		}
	}

	// Update adaptive interval
	if ac.adaptive {
		ac.updateAdaptiveInterval(accounts)
	}

	if ac.metrics != nil {
		ac.metrics.RecordCollector("poll", "success", "active")
	}
}

// fetchWithRetry fetches quota with retry logic
func (ac *ActiveCollector) fetchWithRetry(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
	var lastErr error

	for attempt := 0; attempt <= ac.retryAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(ac.retryBackoff * time.Duration(attempt))
		}

		ctx, cancel := context.WithTimeout(ctx, ac.timeout)
		quota, err := ac.fetcher.FetchQuota(ctx, accountID)
		cancel()

		if err == nil {
			return quota, nil
		}

		lastErr = err
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", ac.retryAttempts+1, lastErr)
}

// updateAdaptiveInterval adjusts polling interval based on quota levels
func (ac *ActiveCollector) updateAdaptiveInterval(accounts []*models.Account) {
	if len(accounts) == 0 {
		return
	}

	// Calculate average effective remaining percentage
	var totalPct float64
	count := 0

	for _, acc := range accounts {
		quota, ok := ac.store.GetQuota(acc.ID)
		if !ok {
			continue
		}
		totalPct += quota.EffectiveRemainingPct
		count++
	}

	if count == 0 {
		return
	}

	avgPct := totalPct / float64(count)

	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.lastQuotaPct = avgPct

	// Adjust interval based on quota level
	// Lower quota = more frequent polling
	switch {
	case avgPct < 20: // Critical - poll frequently
		ac.currentInterval = ac.interval / 4
	case avgPct < 50: // Warning - poll more often
		ac.currentInterval = ac.interval / 2
	case avgPct < 80: // Normal
		ac.currentInterval = ac.interval
	default: // Healthy - poll less frequently
		ac.currentInterval = ac.interval * 2
	}

	// Clamp interval
	if ac.currentInterval < 5*time.Second {
		ac.currentInterval = 5 * time.Second
	}
	if ac.currentInterval > 5*time.Minute {
		ac.currentInterval = 5 * time.Minute
	}
}

// getInterval returns the current polling interval
func (ac *ActiveCollector) getInterval() time.Duration {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.currentInterval
}

// GetInterval returns the current polling interval (public)
func (ac *ActiveCollector) GetInterval() time.Duration {
	return ac.getInterval()
}

// GetCircuitBreakerState returns the circuit breaker state
func (ac *ActiveCollector) GetCircuitBreakerState() CircuitState {
	if !ac.cbEnabled || ac.cb == nil {
		return CircuitClosed
	}
	return ac.cb.State()
}
