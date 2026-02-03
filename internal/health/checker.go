package health

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// HealthStore interface extends the store with health-related operations.
type HealthStore interface {
	GetAccount(id string) (*models.Account, bool)
	ListAccounts() []*models.Account
	ListEnabledAccounts() []*models.Account
	GetHealthStatus(accountID string) (*models.HealthStatus, bool)
	SetHealthStatus(status *models.HealthStatus)
	GetBaseline(accountID string) *Baseline
	SetBaseline(baseline *Baseline)
}

// Config contains the health checker configuration.
type Config struct {
	Interval      time.Duration
	Timeout       time.Duration
	LatencySpike  float64
	P95Multiplier float64
	QualityChecks bool
}

// CheckResult represents the result of a health check.
type CheckResult struct {
	AccountID     string        `json:"account_id"`
	Status        string        `json:"status"`
	Latency       time.Duration `json:"latency"`
	ErrorRate     float64       `json:"error_rate"`
	Anomalies     []*Anomaly    `json:"anomalies,omitempty"`
	ShadowBanRisk ShadowBanRisk `json:"shadow_ban_risk"`
	CheckDuration time.Duration `json:"check_duration"`
	CheckedAt     time.Time     `json:"checked_at"`
	Error         string        `json:"error,omitempty"`
}

// Checker performs health checks on accounts.
type Checker struct {
	cfg               Config
	store             HealthStore
	detector          *AnomalyDetector
	shadowBanDetector *ShadowBanDetector
	baselines         map[string]*Baseline
	mu                sync.RWMutex
	stopCh            chan struct{}
	wg                sync.WaitGroup
	running           bool
	muRun             sync.Mutex
	providerClient    *ProviderClient
}

// NewChecker creates a new health checker.
func NewChecker(cfg Config, store HealthStore) *Checker {
	checker := &Checker{
		cfg:               cfg,
		store:             store,
		detector:          NewAnomalyDetector(cfg.LatencySpike, cfg.P95Multiplier),
		shadowBanDetector: NewShadowBanDetector(),
		baselines:         make(map[string]*Baseline),
		stopCh:            make(chan struct{}),
	}

	// Create provider client with default timeout
	providerTimeout := cfg.Timeout
	if providerTimeout == 0 {
		providerTimeout = 5 * time.Second
	}
	checker.providerClient = NewProviderClient(providerTimeout)

	// Set default endpoints
	checker.providerClient.SetEndpoints(DefaultProviderEndpoints())

	return checker
}

// CheckAccount performs a health check on a single account.
func (c *Checker) CheckAccount(ctx context.Context, accountID string) *CheckResult {
	start := time.Now()
	result := &CheckResult{
		AccountID: accountID,
		CheckedAt: start,
	}

	// Get account
	account, ok := c.store.GetAccount(accountID)
	if !ok || !account.Enabled {
		result.Status = "disabled"
		result.CheckDuration = time.Since(start)
		return result
	}

	// Get health status
	healthStatus, _ := c.store.GetHealthStatus(accountID)
	if healthStatus == nil {
		healthStatus = &models.HealthStatus{
			AccountID: accountID,
		}
	}

	// Get baseline
	baseline := c.GetBaseline(accountID)
	if baseline == nil {
		baseline = NewBaseline(accountID)
		c.SetBaseline(baseline)
	}

	// Perform the health check with timeout
	checkCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	// Simulate health check (in real implementation, this would make an actual request)
	latency := c.performHealthCheck(checkCtx, account)

	result.Latency = latency
	result.ErrorRate = healthStatus.ErrorRate

	// Check for anomalies
	anomalies := c.detector.DetectAnomaly(baseline, latency, healthStatus.ErrorRate, 0)
	if len(anomalies) > 0 {
		result.Anomalies = anomalies
		result.Status = "degraded"
	} else {
		result.Status = "healthy"
	}

	// Check shadow ban risk
	risk := c.shadowBanDetector.CheckShadowBanRisk(
		baseline,
		healthStatus.ConsecutiveErrors,
		healthStatus.TotalRequests,
		healthStatus.FailedRequests,
		latency,
	)
	result.ShadowBanRisk = risk

	if c.shadowBanDetector.IsShadowBanned(risk) {
		result.Status = "shadow_banned"
	}

	// Update health status
	healthStatus.CurrentLatency = latency
	healthStatus.LastCheckedAt = start
	healthStatus.ShadowBanRisk = risk.GetRiskLevel()
	healthStatus.IsShadowBanned = c.shadowBanDetector.IsShadowBanned(risk)
	c.store.SetHealthStatus(healthStatus)

	// Update baseline
	baseline.UpdateBaseline(latency, result.Status == "healthy")

	result.CheckDuration = time.Since(start)

	return result
}

// performHealthCheck performs the actual health check by making HTTP requests to providers.
func (c *Checker) performHealthCheck(ctx context.Context, account *models.Account) time.Duration {
	if c.providerClient == nil {
		log.Printf("health: providerClient not initialized for account %s", account.ID)
		return 50 * time.Millisecond
	}

	log.Printf("health: checking provider %s for account %s", account.Provider, account.ID)

	result, err := c.providerClient.CheckHealth(ctx, account.Provider)
	if err != nil {
		log.Printf("health: error checking provider %s for account %s: %v", account.Provider, account.ID, err)
		return c.cfg.Timeout // Return timeout as latency on error
	}

	if result.Available {
		log.Printf("health: provider %s for account %s is available, latency: %v", account.Provider, account.ID, result.Latency)
	} else {
		log.Printf("health: provider %s for account %s is unavailable: %s", account.Provider, account.ID, result.Message)
	}

	return result.Latency
}

// CheckAll performs health checks on all enabled accounts.
func (c *Checker) CheckAll(ctx context.Context) []*CheckResult {
	accounts := c.store.ListEnabledAccounts()
	results := make([]*CheckResult, 0, len(accounts))

	// Use semaphore for concurrent checks
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, account := range accounts {
		wg.Add(1)
		go func(acc *models.Account) {
			defer wg.Done()

			sem <- struct{}{}        // Acquire semaphore
			defer func() { <-sem }() // Release semaphore

			result := c.CheckAccount(ctx, acc.ID)

			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(account)
	}

	wg.Wait()

	return results
}

// DetectAnomalies detects anomalies for a specific account.
func (c *Checker) DetectAnomalies(accountID string, metrics *models.HealthStatus) []*Anomaly {
	baseline := c.GetBaseline(accountID)
	if baseline == nil {
		return nil
	}

	return c.detector.DetectAnomaly(
		baseline,
		metrics.CurrentLatency,
		metrics.ErrorRate,
		0,
	)
}

// CalculateBaseline calculates the baseline metrics for an account.
func (c *Checker) CalculateBaseline(accountID string) *Baseline {
	baseline := c.GetBaseline(accountID)
	if baseline == nil {
		baseline = NewBaseline(accountID)
		c.SetBaseline(baseline)
	}
	return baseline
}

// DetectShadowBan detects shadow ban risk for an account.
func (c *Checker) DetectShadowBan(ctx context.Context, accountID string) ShadowBanRisk {
	healthStatus, _ := c.store.GetHealthStatus(accountID)
	if healthStatus == nil {
		healthStatus = &models.HealthStatus{
			AccountID: accountID,
		}
	}

	baseline := c.GetBaseline(accountID)
	if baseline == nil {
		baseline = NewBaseline(accountID)
		c.SetBaseline(baseline)
	}

	return c.shadowBanDetector.CheckShadowBanRisk(
		baseline,
		healthStatus.ConsecutiveErrors,
		healthStatus.TotalRequests,
		healthStatus.FailedRequests,
		healthStatus.CurrentLatency,
	)
}

// Start starts the periodic health check loop.
func (c *Checker) Start(ctx context.Context) {
	c.muRun.Lock()
	if c.running {
		c.muRun.Unlock()
		return
	}
	c.running = true
	c.muRun.Unlock()

	c.wg.Add(1)
	go c.run(ctx)
}

// run is the main loop for periodic health checks.
func (c *Checker) run(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			results := c.CheckAll(ctx)
			c.processCheckResults(results)
		}
	}
}

// processCheckResults processes the results of health checks.
func (c *Checker) processCheckResults(results []*CheckResult) {
	for _, result := range results {
		// Update health status in store
		healthStatus, _ := c.store.GetHealthStatus(result.AccountID)
		if healthStatus == nil {
			healthStatus = &models.HealthStatus{
				AccountID: result.AccountID,
			}
		}

		healthStatus.LastCheckedAt = result.CheckedAt
		healthStatus.CurrentLatency = result.Latency
		healthStatus.ShadowBanRisk = result.ShadowBanRisk.GetRiskLevel()
		healthStatus.IsShadowBanned = c.shadowBanDetector.IsShadowBanned(result.ShadowBanRisk)

		c.store.SetHealthStatus(healthStatus)

		// Update baseline
		baseline := c.GetBaseline(result.AccountID)
		if baseline != nil {
			baseline.UpdateBaseline(result.Latency, result.Status == "healthy")
		}
	}
}

// Stop stops the health checker gracefully.
func (c *Checker) Stop() {
	c.muRun.Lock()
	if !c.running {
		c.muRun.Unlock()
		return
	}
	c.running = false
	c.muRun.Unlock()

	close(c.stopCh)
	c.wg.Wait()
}

// IsRunning returns whether the checker is running.
func (c *Checker) IsRunning() bool {
	c.muRun.Lock()
	defer c.muRun.Unlock()
	return c.running
}

// GetBaseline retrieves the baseline for an account.
func (c *Checker) GetBaseline(accountID string) *Baseline {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baselines[accountID]
}

// SetBaseline sets the baseline for an account.
func (c *Checker) SetBaseline(baseline *Baseline) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baselines[baseline.AccountID] = baseline
}

// GetAnomalyDetector returns the anomaly detector.
func (c *Checker) GetAnomalyDetector() *AnomalyDetector {
	return c.detector
}

// GetShadowBanDetector returns the shadow ban detector.
func (c *Checker) GetShadowBanDetector() *ShadowBanDetector {
	return c.shadowBanDetector
}

// SetConfig updates the checker configuration.
func (c *Checker) SetConfig(cfg Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
	c.detector = NewAnomalyDetector(cfg.LatencySpike, cfg.P95Multiplier)
}

// AddControlPrompt adds a control prompt for quality checks.
func (c *Checker) AddControlPrompt(prompt ControlPrompt) {
	c.shadowBanDetector.ControlPrompts = append(c.shadowBanDetector.ControlPrompts, prompt)
	c.shadowBanDetector.QualityCheckEnabled = true
}

// GetCheckResults returns recent check results (in-memory storage).
func (c *Checker) GetCheckResults() []*CheckResult {
	// This is a placeholder - in real implementation, you'd want to store results
	return []*CheckResult{}
}

// SetProviderEndpoints sets provider endpoints from configuration.
func (c *Checker) SetProviderEndpoints(endpoints map[string]string) {
	if c.providerClient == nil {
		log.Printf("health: providerClient not initialized, cannot set endpoints")
		return
	}

	for providerStr, endpoint := range endpoints {
		c.providerClient.AddEndpoint(models.Provider(providerStr), endpoint)
	}
	log.Printf("health: configured %d provider endpoints", len(endpoints))
}

// GetProviderClient returns the provider client.
func (c *Checker) GetProviderClient() *ProviderClient {
	return c.providerClient
}
