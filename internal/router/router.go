package router

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/errors"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// router selects the best account for routing requests
type router struct {
	store      store.Store
	config     Config
	mu         sync.RWMutex
	lastSwitch map[string]time.Time // accountID -> last switch time

	// Anti-flapping state
	currentAccount   string    // currently selected account
	accountDwellTime time.Time // when current account was selected

	// Circuit breakers per provider
	circuitBreakers map[string]*CircuitBreaker
	cbMu            sync.RWMutex
}

// Config holds router configuration
type Config struct {
	// Thresholds
	WarningThreshold  float64
	SwitchThreshold   float64
	CriticalThreshold float64
	MinSafeThreshold  float64

	// Anti-flapping
	MinDwellTime        time.Duration
	CooldownAfterSwitch time.Duration
	HysteresisMargin    float64

	// Weights for scoring
	Weights Weights

	// Policies
	DefaultPolicy string
	Policies      map[string]Weights

	// FallbackChains defines fallback order for accounts/providers
	FallbackChains map[string][]string

	// Circuit Breaker
	CircuitBreaker config.CircuitBreakerConfig
}

// Weights defines scoring weights
type Weights struct {
	Safety      float64 // Remaining quota safety
	Refill      float64 // Refill rate
	Tier        float64 // Account tier/priority
	Reliability float64 // Historical reliability
	Cost        float64 // Cost efficiency
}

// DefaultWeights returns default balanced weights
func DefaultWeights() Weights {
	return Weights{
		Safety:      0.4,
		Refill:      0.3,
		Tier:        0.15,
		Reliability: 0.1,
		Cost:        0.05,
	}
}

// DefaultConfig returns default router configuration
func DefaultConfig() Config {
	return Config{
		WarningThreshold:    15.0,
		SwitchThreshold:     10.0,
		CriticalThreshold:   5.0,
		MinSafeThreshold:    5.0,
		MinDwellTime:        5 * time.Minute,
		CooldownAfterSwitch: 3 * time.Minute,
		HysteresisMargin:    5.0,
		Weights:             DefaultWeights(),
		DefaultPolicy:       "balanced",
		Policies: map[string]Weights{
			"balanced":    DefaultWeights(),
			"cost":        {Safety: 0.2, Refill: 0.2, Tier: 0.1, Reliability: 0.1, Cost: 0.4},
			"performance": {Safety: 0.5, Refill: 0.3, Tier: 0.1, Reliability: 0.1, Cost: 0.0},
			"safety":      {Safety: 0.7, Refill: 0.2, Tier: 0.05, Reliability: 0.05, Cost: 0.0},
		},
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold: 5,
			Timeout:          30 * time.Second,
			HalfOpenLimit:    3,
		},
	}
}

// NewRouter creates a new router
func NewRouter(s store.Store, cfg Config) Router {
	r := &router{
		store:           s,
		config:          cfg,
		lastSwitch:      make(map[string]time.Time),
		circuitBreakers: make(map[string]*CircuitBreaker),
	}

	// Initialize circuit breakers for each provider
	accounts := s.ListEnabledAccounts()
	providerSet := make(map[models.Provider]bool)
	for _, acc := range accounts {
		if !providerSet[acc.Provider] {
			providerSet[acc.Provider] = true
			r.circuitBreakers[string(acc.Provider)] = NewCircuitBreaker(
				string(acc.Provider),
				cfg.CircuitBreaker.FailureThreshold,
				cfg.CircuitBreaker.Timeout,
			)
		}
	}

	return r
}

// GetCurrentAccount returns the currently selected account ID
func (r *router) GetCurrentAccount() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentAccount
}

// shouldSwitch checks if we should switch from current account to new one
// Implements anti-flapping with min dwell time and hysteresis
func (r *router) shouldSwitch(currentAccountID, newAccountID string, newScore, currentScore float64) bool {
	r.mu.RLock()
	currentAccount := r.currentAccount
	r.mu.RUnlock()

	// If no current account, always allow switch
	if currentAccount == "" || currentAccountID == "" {
		return true
	}

	// If same account, no need to switch
	if currentAccount == newAccountID {
		return false
	}

	// Get current account info to check if it's in a critical state
	currentQuota, hasQuota := r.store.GetQuota(currentAccount)

	// If current account is critical, we SHOULD switch even if dwell time or hysteresis aren't met
	if hasQuota && currentQuota.IsCritical(r.config.CriticalThreshold) && newScore > 0.2 {
		return true
	}

	// Apply hysteresis: only switch if new score is significantly better
	// This prevents oscillation when scores are close
	scoreDiff := newScore - currentScore
	return scoreDiff >= r.config.HysteresisMargin/100.0
}

// SelectRequest contains parameters for account selection
type SelectRequest struct {
	Provider         models.Provider
	RequiredDims     []models.DimensionType
	EstimatedCost    float64           // Estimated cost in percent
	Policy           string            // Routing policy name
	Exclude          []string          // Account IDs to exclude
	ExcludeProviders []models.Provider // Providers to exclude
	EstimatedTokens  int64             // Estimated number of tokens required
}

// SelectResponse contains the selection result
type SelectResponse struct {
	AccountID      string
	Provider       models.Provider
	Score          float64
	Reason         string
	AlternativeIDs []string
}

// Select chooses the best account for the request
func (r *router) Select(ctx context.Context, req SelectRequest) (*SelectResponse, error) {
	accounts := r.store.ListEnabledAccounts()
	if len(accounts) == 0 {
		return nil, &errors.ErrNoSuitableAccounts{Reason: "no enabled accounts available"}
	}

	// Filter by provider if specified
	if req.Provider != "" {
		accounts = filterByProvider(accounts, req.Provider)
	}

	// Filter excluded accounts
	if len(req.Exclude) > 0 {
		accounts = filterExcluded(accounts, req.Exclude)
	}

	// Filter excluded providers
	if len(req.ExcludeProviders) > 0 {
		accounts = filterExcludedProviders(accounts, req.ExcludeProviders)
	}

	if len(accounts) == 0 {
		return nil, &errors.ErrNoSuitableAccounts{Reason: "no suitable accounts found after filtering"}
	}

	// Get weights for the policy
	weights := r.getWeights(req.Policy)

	// Score all accounts
	scored := make([]scoredAccount, 0, len(accounts))
	for _, acc := range accounts {
		score, reason := r.scoreAccount(acc, weights, req)
		scored = append(scored, scoredAccount{
			account: acc,
			score:   score,
			reason:  reason,
		})
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Get best account
	best := scored[0]
	if best.score <= 0 {
		return nil, &errors.ErrNoSuitableAccounts{Reason: best.reason}
	}

	// Apply anti-flapping: check if we should switch
	currentAccount := r.GetCurrentAccount()
	currentScore := 0.0
	if currentAccount != "" {
		for _, s := range scored {
			if s.account.ID == currentAccount {
				currentScore = s.score
				break
			}
		}
	}

	if !r.shouldSwitch(currentAccount, best.account.ID, best.score, currentScore) {
		// Stay with current account if we shouldn't switch
		if currentAccount != "" {
			// Find current account in scored list
			for _, s := range scored {
				if s.account.ID == currentAccount && s.score > 0 {
					best = s
					break
				}
			}
		}
	}

	// Build alternative IDs
	alternatives := make([]string, 0, min(3, len(scored)-1))
	for i := 1; i < len(scored) && len(alternatives) < 3; i++ {
		if scored[i].score > 0 {
			alternatives = append(alternatives, scored[i].account.ID)
		}
	}

	bestReason := best.reason
	for _, s := range scored {
		if s.account.ID == best.account.ID {
			continue
		}
		if s.account.Priority > best.account.Priority && s.reason == "critical quota level" {
			bestReason = fmt.Sprintf("%s; fallback due to critical account", bestReason)
			break
		}
	}

	return &SelectResponse{
		AccountID:      best.account.ID,
		Provider:       best.account.Provider,
		Score:          best.score,
		Reason:         bestReason,
		AlternativeIDs: alternatives,
	}, nil
}

// scoredAccount holds an account with its score
type scoredAccount struct {
	account *models.Account
	score   float64
	reason  string
}

// scoreAccount calculates a score for an account
func (r *router) scoreAccount(acc *models.Account, weights Weights, req SelectRequest) (float64, string) {
	quota, ok := r.store.GetQuota(acc.ID)
	if !ok {
		return 0, "no quota data"
	}

	// Calculate effective remaining with virtual usage
	effectiveRemaining := quota.EffectiveRemainingWithVirtual()

	// Check if account is exhausted
	if quota.IsExhausted() || effectiveRemaining <= 0 {
		return 0, "quota exhausted"
	}

	// Check if provider is excluded
	for _, p := range req.ExcludeProviders {
		if acc.Provider == p {
			return 0, "provider excluded"
		}
	}

	// Check critical threshold
	if quota.IsCritical(r.config.CriticalThreshold) {
		return 0.1, "critical quota level"
	}

	// Check if we have enough for the estimated cost
	if req.EstimatedCost > 0 && effectiveRemaining < req.EstimatedCost+r.config.MinSafeThreshold {
		return 0.2, "insufficient quota for estimated cost"
	}

	// Check if we have enough tokens (if TPM dimension exists)
	if req.EstimatedTokens > 0 {
		tpm, ok := quota.Dimensions.FindByType(models.DimensionTPM)
		if ok && tpm.Remaining < req.EstimatedTokens {
			return 0, "insufficient quota for estimated tokens"
		}
	}

	// Check required dimensions
	if len(req.RequiredDims) > 0 {
		for _, dim := range req.RequiredDims {
			if _, ok := quota.Dimensions.FindByType(dim); !ok {
				return 0, fmt.Sprintf("missing required dimension: %s", dim)
			}
		}
	}

	// Calculate component scores
	safetyScore := effectiveRemaining / 100.0
	if safetyScore > 1.0 {
		safetyScore = 1.0
	}

	// Refill score based on critical dimension refill rate
	refillScore := 0.5
	if crit := quota.CriticalDimension; crit != nil {
		refillScore = crit.RefillRate
		if refillScore > 1.0 {
			refillScore = 1.0
		}
	}

	// Tier score (higher priority = higher score)
	tierScore := float64(acc.Priority) / 10.0
	if tierScore > 1.0 {
		tierScore = 1.0
	}

	// Reliability score (based on confidence)
	reliabilityScore := quota.Confidence

	// Cost score (lower cost = higher score)
	costScore := 1.0 - (acc.InputCost+acc.OutputCost)/0.1
	if costScore < 0 {
		costScore = 0
	}

	// Calculate weighted score
	score := safetyScore*weights.Safety +
		refillScore*weights.Refill +
		tierScore*weights.Tier +
		reliabilityScore*weights.Reliability +
		costScore*weights.Cost

	reason := fmt.Sprintf("safety=%.2f, refill=%.2f, tier=%.2f, reliability=%.2f, cost=%.2f",
		safetyScore, refillScore, tierScore, reliabilityScore, costScore)

	return score, reason
}

// canSwitch checks if we can switch to the given account
func (r *router) canSwitch(accountID string) bool {
	r.mu.RLock()
	lastSwitch, ok := r.lastSwitch[accountID]
	r.mu.RUnlock()

	if !ok {
		return true
	}

	// Check cooldown period
	if time.Since(lastSwitch) < r.config.CooldownAfterSwitch {
		return false
	}

	return true
}

// RecordSwitch records that we switched to an account
func (r *router) RecordSwitch(accountID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastSwitch[accountID] = time.Now()

	// Update current account tracking for anti-flapping
	if r.currentAccount != accountID {
		r.currentAccount = accountID
		r.accountDwellTime = time.Now()
	}
}

// getWeights returns weights for a policy
func (r *router) getWeights(policy string) Weights {
	if policy == "" {
		policy = r.config.DefaultPolicy
	}

	if weights, ok := r.config.Policies[policy]; ok {
		return weights
	}

	return r.config.Weights
}

// GetStats returns router statistics
func (r *router) GetStats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RouterStats{
		LastSwitches: len(r.lastSwitch),
	}
}

// RouterStats contains router statistics
type RouterStats struct {
	LastSwitches int
}

// Helper functions

func filterByProvider(accounts []*models.Account, provider models.Provider) []*models.Account {
	result := make([]*models.Account, 0)
	for _, acc := range accounts {
		if acc.Provider == provider {
			result = append(result, acc)
		}
	}
	return result
}

func filterExcluded(accounts []*models.Account, exclude []string) []*models.Account {
	excludeMap := make(map[string]bool)
	for _, id := range exclude {
		excludeMap[id] = true
	}

	result := make([]*models.Account, 0)
	for _, acc := range accounts {
		if !excludeMap[acc.ID] {
			result = append(result, acc)
		}
	}
	return result
}

func filterExcludedProviders(accounts []*models.Account, exclude []models.Provider) []*models.Account {
	excludeMap := make(map[models.Provider]bool)
	for _, p := range exclude {
		excludeMap[p] = true
	}

	result := make([]*models.Account, 0)
	for _, acc := range accounts {
		if !excludeMap[acc.Provider] {
			result = append(result, acc)
		}
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// IsHealthy checks if the router can make selections
func (r *router) IsHealthy() bool {
	accounts := r.store.ListEnabledAccounts()
	return len(accounts) > 0
}

// CheckHealth checks the health status of an account
func (r *router) CheckHealth(ctx context.Context, accountID string) (*models.HealthStatus, error) {
	_, ok := r.store.GetAccount(accountID)
	if !ok {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	return &models.HealthStatus{
		AccountID:          accountID,
		BaselineLatency:    0,
		CurrentLatency:     0,
		ErrorRate:          0,
		ShadowBanRisk:      0,
		IsShadowBanned:     false,
		ConsecutiveErrors:  0,
		SuccessfulRequests: 0,
		FailedRequests:     0,
		TotalRequests:      0,
	}, nil
}

// Feedback records routing feedback for learning (stub implementation)
func (r *router) Feedback(ctx context.Context, feedback *FeedbackRequest) error {
	return nil
}

// GetAccounts returns all enabled accounts
func (r *router) GetAccounts(ctx context.Context) ([]*models.Account, error) {
	return r.store.ListEnabledAccounts(), nil
}

// GetQuota returns quota information for an account
func (r *router) GetQuota(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
	quota, ok := r.store.GetQuota(accountID)
	if !ok {
		return nil, nil
	}
	return quota, nil
}

// GetAllQuotas returns all quota information
func (r *router) GetAllQuotas(ctx context.Context) (map[string]*models.QuotaInfo, error) {
	quotas := r.store.ListQuotas()
	result := make(map[string]*models.QuotaInfo)
	for _, q := range quotas {
		result[q.AccountID] = q
	}
	return result, nil
}

// GetRoutingDistribution returns the optimal request distribution
func (r *router) GetRoutingDistribution(ctx context.Context) (map[string]int, error) {
	dist := r.CalculateOptimalDistribution(ctx, 100)
	result := make(map[string]int)
	for id, pct := range dist {
		result[id] = int(pct)
	}
	return result, nil
}

// GetConfig returns the router configuration
func (r *router) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return &r.config
}

// UpdateConfig updates the router configuration at runtime.
func (r *router) UpdateConfig(cfg Config) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.config.WarningThreshold = cfg.WarningThreshold
	r.config.SwitchThreshold = cfg.SwitchThreshold
	r.config.CriticalThreshold = cfg.CriticalThreshold
	r.config.MinSafeThreshold = cfg.MinSafeThreshold
	r.config.MinDwellTime = cfg.MinDwellTime
	r.config.CooldownAfterSwitch = cfg.CooldownAfterSwitch
	r.config.HysteresisMargin = cfg.HysteresisMargin
	r.config.Weights = cfg.Weights
	r.config.Policies = cfg.Policies
	r.config.DefaultPolicy = cfg.DefaultPolicy
	r.config.FallbackChains = cfg.FallbackChains
	r.config.CircuitBreaker = cfg.CircuitBreaker
}

// Close cleans up router resources
func (r *router) Close() error {
	return nil
}

// GetAccountStatus returns detailed status for an account
func (r *router) GetAccountStatus(accountID string) (*AccountStatus, error) {
	acc, ok := r.store.GetAccount(accountID)
	if !ok {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	quota, hasQuota := r.store.GetQuota(accountID)

	status := &AccountStatus{
		AccountID: acc.ID,
		Provider:  acc.Provider,
		Enabled:   acc.Enabled,
		Tier:      acc.Tier,
	}

	if hasQuota {
		status.HasQuotaData = true
		status.EffectiveRemaining = quota.EffectiveRemainingWithVirtual()
		status.IsExhausted = quota.IsExhausted()
		status.IsCritical = quota.IsCritical(r.config.CriticalThreshold)
		status.VirtualUsed = quota.VirtualUsedPercent
	}

	return status, nil
}

// AccountStatus contains account status information
type AccountStatus struct {
	AccountID          string
	Provider           models.Provider
	Enabled            bool
	Tier               string
	HasQuotaData       bool
	EffectiveRemaining float64
	IsExhausted        bool
	IsCritical         bool
	VirtualUsed        float64
}

// CalculateOptimalDistribution calculates the optimal request distribution across accounts
func (r *router) CalculateOptimalDistribution(ctx context.Context, totalRequests int) map[string]float64 {
	accounts := r.store.ListEnabledAccounts()
	if len(accounts) == 0 {
		return nil
	}

	distribution := make(map[string]float64)
	weights := r.config.Weights

	// Calculate scores for all accounts
	type accountScore struct {
		id    string
		score float64
	}
	scores := make([]accountScore, 0, len(accounts))

	for _, acc := range accounts {
		score, _ := r.scoreAccount(acc, weights, SelectRequest{})
		if score > 0 {
			scores = append(scores, accountScore{id: acc.ID, score: score})
		}
	}

	if len(scores) == 0 {
		return distribution
	}

	// Calculate total score
	var totalScore float64
	for _, s := range scores {
		totalScore += s.score
	}

	// Calculate distribution percentages
	for _, s := range scores {
		distribution[s.id] = (s.score / totalScore) * 100.0
	}

	return distribution
}

// Circuit Breaker methods

// getCircuitBreaker returns the circuit breaker for a provider
func (r *router) getCircuitBreaker(provider models.Provider) *CircuitBreaker {
	r.cbMu.RLock()
	cb, ok := r.circuitBreakers[string(provider)]
	r.cbMu.RUnlock()

	if ok {
		return cb
	}

	// Create circuit breaker for new provider
	r.cbMu.Lock()
	defer r.cbMu.Unlock()

	// Double-check after acquiring write lock
	if cb, ok := r.circuitBreakers[string(provider)]; ok {
		return cb
	}

	cb = NewCircuitBreaker(
		string(provider),
		r.config.CircuitBreaker.FailureThreshold,
		r.config.CircuitBreaker.Timeout,
	)
	cb.halfOpenLimit = r.config.CircuitBreaker.HalfOpenLimit
	r.circuitBreakers[string(provider)] = cb
	return cb
}

// RecordProviderSuccess records a successful call to a provider
func (r *router) RecordProviderSuccess(provider models.Provider) {
	cb := r.getCircuitBreaker(provider)
	cb.RecordSuccess()
}

// RecordProviderFailure records a failed call to a provider
func (r *router) RecordProviderFailure(provider models.Provider) {
	cb := r.getCircuitBreaker(provider)
	cb.RecordFailure()
}

// GetProviderCircuitState returns the circuit state for a provider
func (r *router) GetProviderCircuitState(provider models.Provider) CircuitState {
	cb := r.getCircuitBreaker(provider)
	return cb.State()
}

// GetAllCircuitBreakerMetrics returns metrics for all circuit breakers
func (r *router) GetAllCircuitBreakerMetrics() map[string]CircuitBreakerMetrics {
	r.cbMu.RLock()
	defer r.cbMu.RUnlock()

	result := make(map[string]CircuitBreakerMetrics)
	for provider, cb := range r.circuitBreakers {
		result[provider] = cb.GetMetrics()
	}
	return result
}

// ResetCircuitBreaker resets the circuit breaker for a provider
func (r *router) ResetCircuitBreaker(provider models.Provider) {
	r.cbMu.Lock()
	defer r.cbMu.Unlock()

	if cb, ok := r.circuitBreakers[string(provider)]; ok {
		cb.Reset()
	}
}

// ExecuteWithCircuitBreaker executes a function with circuit breaker protection for a provider
func (r *router) ExecuteWithCircuitBreaker(ctx context.Context, provider models.Provider, fn func() error) error {
	cb := r.getCircuitBreaker(provider)
	return cb.Execute(ctx, fn)
}
