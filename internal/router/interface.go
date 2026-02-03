package router

import (
	"context"

	"github.com/quotaguard/quotaguard/internal/models"
)

// Router interface defines the contract for routing requests to accounts.
// This interface enables dependency injection and testing with mock implementations.
type Router interface {
	// Select chooses the best account for the request
	Select(ctx context.Context, req SelectRequest) (*SelectResponse, error)

	// Feedback records routing feedback for learning
	Feedback(ctx context.Context, feedback *FeedbackRequest) error

	// GetAccounts returns all enabled accounts
	GetAccounts(ctx context.Context) ([]*models.Account, error)

	// GetQuota returns quota information for an account
	GetQuota(ctx context.Context, accountID string) (*models.QuotaInfo, error)

	// GetAllQuotas returns all quota information
	GetAllQuotas(ctx context.Context) (map[string]*models.QuotaInfo, error)

	// GetRoutingDistribution returns the optimal request distribution
	GetRoutingDistribution(ctx context.Context) (map[string]int, error)

	// CheckHealth checks the health status of an account
	CheckHealth(ctx context.Context, accountID string) (*models.HealthStatus, error)

	// GetConfig returns the router configuration
	GetConfig() *Config

	// UpdateConfig applies a new router configuration at runtime
	UpdateConfig(cfg Config)

	// Close cleans up router resources
	Close() error

	// IsHealthy checks if the router can make selections
	IsHealthy() bool

	// RecordSwitch records that we switched to an account
	RecordSwitch(accountID string)

	// GetAccountStatus returns detailed status for an account
	GetAccountStatus(accountID string) (*AccountStatus, error)

	// CalculateOptimalDistribution calculates the optimal request distribution
	CalculateOptimalDistribution(ctx context.Context, totalRequests int) map[string]float64

	// GetCurrentAccount returns the currently selected account ID
	GetCurrentAccount() string

	// GetStats returns router statistics
	GetStats() RouterStats

	// Circuit Breaker methods

	// RecordProviderSuccess records a successful call to a provider
	RecordProviderSuccess(provider models.Provider)

	// RecordProviderFailure records a failed call to a provider
	RecordProviderFailure(provider models.Provider)

	// GetProviderCircuitState returns the circuit state for a provider
	GetProviderCircuitState(provider models.Provider) CircuitState

	// GetAllCircuitBreakerMetrics returns metrics for all circuit breakers
	GetAllCircuitBreakerMetrics() map[string]CircuitBreakerMetrics

	// ResetCircuitBreaker resets the circuit breaker for a provider
	ResetCircuitBreaker(provider models.Provider)

	// ExecuteWithCircuitBreaker executes a function with circuit breaker protection
	ExecuteWithCircuitBreaker(ctx context.Context, provider models.Provider, fn func() error) error
}

// FeedbackRequest represents feedback about a routing decision
type FeedbackRequest struct {
	AccountID     string  `json:"account_id"`
	ReservationID string  `json:"reservation_id,omitempty"`
	ActualCost    float64 `json:"actual_cost_percent,omitempty"`
	Success       bool    `json:"success"`
	Error         string  `json:"error,omitempty"`
	Latency       int64   `json:"latency_ms,omitempty"`
}
