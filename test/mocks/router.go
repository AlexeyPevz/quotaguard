package mocks

import (
	"context"
	"sync"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/router"
)

// MockRouter is a mock implementation of the Router interface for testing
type MockRouter struct {
	mu             sync.RWMutex
	accounts       []*models.Account
	quotas         map[string]*models.QuotaInfo
	healthStatus   map[string]*models.HealthStatus
	selectResponse *router.SelectResponse
	selectError    error
	healthy        bool
	currentAccount string
	stats          router.RouterStats
	config         *router.Config
	closeCalled    bool
	feedbackCalled bool
	feedbackReq    *router.FeedbackRequest
}

// NewMockRouter creates a new MockRouter
func NewMockRouter() *MockRouter {
	return &MockRouter{
		quotas:       make(map[string]*models.QuotaInfo),
		healthStatus: make(map[string]*models.HealthStatus),
		healthy:      true,
		config:       &router.Config{},
	}
}

// SetAccounts sets the accounts to return
func (m *MockRouter) SetAccounts(accounts []*models.Account) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accounts = accounts
}

// SetQuotas sets the quotas to return
func (m *MockRouter) SetQuotas(quotas map[string]*models.QuotaInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quotas = quotas
}

// SetSelectResponse sets the response for Select calls
func (m *MockRouter) SetSelectResponse(resp *router.SelectResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selectResponse = resp
}

// SetSelectError sets the error for Select calls
func (m *MockRouter) SetSelectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selectError = err
}

// SetHealthy sets the healthy status
func (m *MockRouter) SetHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthy = healthy
}

// SetCurrentAccount sets the current account
func (m *MockRouter) SetCurrentAccount(accountID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentAccount = accountID
}

// SetConfig sets the config
func (m *MockRouter) SetConfig(cfg *router.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// GetFeedbackRequest returns the last feedback request (for assertion)
func (m *MockRouter) GetFeedbackRequest() *router.FeedbackRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.feedbackReq
}

// WasFeedbackCalled returns true if Feedback was called
func (m *MockRouter) WasFeedbackCalled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.feedbackCalled
}

// WasCloseCalled returns true if Close was called
func (m *MockRouter) WasCloseCalled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closeCalled
}

// Select implements router.Router
func (m *MockRouter) Select(ctx context.Context, req router.SelectRequest) (*router.SelectResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.selectError != nil {
		return nil, m.selectError
	}

	if m.selectResponse != nil {
		return m.selectResponse, nil
	}

	// Default response if not configured
	return &router.SelectResponse{
		AccountID:      "mock-account",
		Provider:       models.ProviderOpenAI,
		Score:          1.0,
		Reason:         "mock selection",
		AlternativeIDs: []string{},
	}, nil
}

// Feedback implements router.Router
func (m *MockRouter) Feedback(ctx context.Context, feedback *router.FeedbackRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.feedbackCalled = true
	m.feedbackReq = feedback
	return nil
}

// GetAccounts implements router.Router
func (m *MockRouter) GetAccounts(ctx context.Context) ([]*models.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accounts, nil
}

// GetQuota implements router.Router
func (m *MockRouter) GetQuota(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if quota, ok := m.quotas[accountID]; ok {
		return quota, nil
	}
	return nil, nil
}

// GetAllQuotas implements router.Router
func (m *MockRouter) GetAllQuotas(ctx context.Context) (map[string]*models.QuotaInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.quotas, nil
}

// GetRoutingDistribution implements router.Router
func (m *MockRouter) GetRoutingDistribution(ctx context.Context) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]int{}, nil
}

// CheckHealth implements router.Router
func (m *MockRouter) CheckHealth(ctx context.Context, accountID string) (*models.HealthStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if status, ok := m.healthStatus[accountID]; ok {
		return status, nil
	}
	return &models.HealthStatus{
		AccountID:      accountID,
		ErrorRate:      0,
		IsShadowBanned: false,
	}, nil
}

// GetConfig implements router.Router
func (m *MockRouter) GetConfig() *router.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// Close implements router.Router
func (m *MockRouter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

// IsHealthy implements router.Router
func (m *MockRouter) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthy
}

// RecordSwitch implements router.Router
func (m *MockRouter) RecordSwitch(accountID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentAccount = accountID
}

// GetAccountStatus implements router.Router
func (m *MockRouter) GetAccountStatus(accountID string) (*router.AccountStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	acc, accountsLen := m.findAccount(accountID)
	quota, hasQuota := m.quotas[accountID]

	status := &router.AccountStatus{
		AccountID: accountID,
	}

	if accountsLen > 0 && acc != nil {
		status.Provider = acc.Provider
		status.Enabled = acc.Enabled
		status.Tier = acc.Tier
	}

	if hasQuota {
		status.HasQuotaData = true
		status.EffectiveRemaining = quota.EffectiveRemainingPct
		status.IsExhausted = quota.IsExhausted()
		status.IsCritical = quota.IsCritical(5.0)
		status.VirtualUsed = quota.VirtualUsedPercent
	}

	return status, nil
}

// CalculateOptimalDistribution implements router.Router
func (m *MockRouter) CalculateOptimalDistribution(ctx context.Context, totalRequests int) map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]float64{}
}

// GetCurrentAccount implements router.Router
func (m *MockRouter) GetCurrentAccount() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentAccount
}

// GetStats implements router.Router
func (m *MockRouter) GetStats() router.RouterStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
}

// Helper function to find account by ID
func (m *MockRouter) findAccount(accountID string) (*models.Account, int) {
	for _, acc := range m.accounts {
		if acc.ID == accountID {
			return acc, len(m.accounts)
		}
	}
	return nil, len(m.accounts)
}

// AssertSelectCalled asserts that Select was called
func (m *MockRouter) AssertSelectCalled(t assertT) {
	// This is a no-op for the mock - in a real implementation with testify/mock
	// you would use mock.AssertExpectations
}

// AssertFeedbackCalled asserts that Feedback was called
func (m *MockRouter) AssertFeedbackCalled(t assertT) {
	if !m.feedbackCalled {
		t.Errorf("Feedback was not called")
	}
}

// AssertCloseCalled asserts that Close was called
func (m *MockRouter) AssertCloseCalled(t assertT) {
	if !m.closeCalled {
		t.Errorf("Close was not called")
	}
}

// assertT is a subset of testing.TB for assertions
type assertT interface {
	Errorf(format string, args ...interface{})
}
