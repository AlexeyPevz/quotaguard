package reservation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// Manager handles soft reservations for quota usage.
type Manager struct {
	store      store.Store
	defaultTTL time.Duration
	opMu       sync.Mutex
	mu         sync.RWMutex

	// Metrics
	metrics *Metrics
}

// Metrics holds reservation-related metrics.
type Metrics struct {
	CreatedTotal   int64
	ReleasedTotal  int64
	ExpiredTotal   int64
	CancelledTotal int64
	ActiveCount    int64
}

// Config holds configuration for the reservation manager.
type Config struct {
	DefaultTTL time.Duration
}

// DefaultConfig returns default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultTTL: 5 * time.Minute,
	}
}

// NewManager creates a new reservation manager.
func NewManager(s store.Store, cfg Config) *Manager {
	return &Manager{
		store:      s,
		defaultTTL: cfg.DefaultTTL,
		metrics:    &Metrics{},
	}
}

// Create creates a new reservation for the given account.
func (m *Manager) Create(ctx context.Context, accountID string, estimatedCostPct float64, correlationID string) (*models.Reservation, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if estimatedCostPct < 0 || estimatedCostPct > 100 {
		return nil, fmt.Errorf("estimated cost must be between 0 and 100, got %f", estimatedCostPct)
	}

	// Check if account exists and has enough quota
	quota, ok := m.store.GetQuota(accountID)
	if !ok {
		return nil, fmt.Errorf("no quota data for account %s", accountID)
	}

	// Check if we have enough effective remaining quota
	if quota.EffectiveRemainingWithVirtual() < estimatedCostPct {
		return nil, fmt.Errorf("insufficient quota: need %.2f%%, have %.2f%%",
			estimatedCostPct, quota.EffectiveRemainingWithVirtual())
	}

	reservation := &models.Reservation{
		ID:               uuid.New().String(),
		AccountID:        accountID,
		EstimatedCostPct: estimatedCostPct,
		Status:           models.ReservationActive,
		CreatedAt:        time.Now(),
		ExpiresAt:        time.Now().Add(m.defaultTTL),
		CorrelationID:    correlationID,
	}

	if err := reservation.Validate(); err != nil {
		return nil, fmt.Errorf("invalid reservation: %w", err)
	}

	// Add virtual usage to quota
	quota.AddVirtualUsed(estimatedCostPct)
	m.store.SetQuota(accountID, quota)

	// Store reservation
	m.store.SetReservation(reservation.ID, reservation)

	// Update metrics
	m.mu.Lock()
	m.metrics.CreatedTotal++
	m.metrics.ActiveCount++
	m.mu.Unlock()

	return reservation, nil
}

// Get retrieves a reservation by ID.
func (m *Manager) Get(reservationID string) (*models.Reservation, bool) {
	return m.store.GetReservation(reservationID)
}

// Release releases a reservation with the actual cost.
func (m *Manager) Release(reservationID string, actualCostPct float64) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	res, ok := m.store.GetReservation(reservationID)
	if !ok {
		return fmt.Errorf("reservation %s not found", reservationID)
	}

	if err := res.Release(actualCostPct); err != nil {
		return err
	}

	// Update reservation
	m.store.SetReservation(reservationID, res)

	// Adjust quota virtual usage
	quota, ok := m.store.GetQuota(res.AccountID)
	if ok {
		quota.ReleaseVirtualUsed(res.EstimatedCostPct)
		quota.AddVirtualUsed(actualCostPct)
		m.store.SetQuota(res.AccountID, quota)
	}

	// Update metrics
	m.mu.Lock()
	m.metrics.ReleasedTotal++
	m.metrics.ActiveCount--
	m.mu.Unlock()

	return nil
}

// Cancel cancels a reservation.
func (m *Manager) Cancel(reservationID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	res, ok := m.store.GetReservation(reservationID)
	if !ok {
		return fmt.Errorf("reservation %s not found", reservationID)
	}

	if err := res.Cancel(); err != nil {
		return err
	}

	// Update reservation
	m.store.SetReservation(reservationID, res)

	// Remove virtual usage from quota
	quota, ok := m.store.GetQuota(res.AccountID)
	if ok {
		quota.ReleaseVirtualUsed(res.EstimatedCostPct)
		m.store.SetQuota(res.AccountID, quota)
	}

	// Update metrics
	m.mu.Lock()
	m.metrics.CancelledTotal++
	m.metrics.ActiveCount--
	m.mu.Unlock()

	return nil
}

// Expire marks a reservation as expired.
func (m *Manager) Expire(reservationID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	res, ok := m.store.GetReservation(reservationID)
	if !ok {
		return fmt.Errorf("reservation %s not found", reservationID)
	}

	if err := res.Expire(); err != nil {
		return err
	}

	// Update reservation
	m.store.SetReservation(reservationID, res)

	// Remove virtual usage from quota
	quota, ok := m.store.GetQuota(res.AccountID)
	if ok {
		quota.ReleaseVirtualUsed(res.EstimatedCostPct)
		m.store.SetQuota(res.AccountID, quota)
	}

	// Update metrics
	m.mu.Lock()
	m.metrics.ExpiredTotal++
	m.metrics.ActiveCount--
	m.mu.Unlock()

	return nil
}

// CleanupExpired removes all expired reservations and returns the count.
func (m *Manager) CleanupExpired() int {
	reservations := m.store.ListReservations()
	expiredCount := 0

	for _, res := range reservations {
		if res.IsExpired() && res.Status == models.ReservationActive {
			if err := m.Expire(res.ID); err == nil {
				expiredCount++
			}
		}
	}

	return expiredCount
}

// ReleaseAll releases all active reservations.
func (m *Manager) ReleaseAll(ctx context.Context) error {
	reservations := m.store.ListReservations()
	var errs []error

	for _, res := range reservations {
		if res == nil || !res.IsActive() {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := m.Cancel(res.ID); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("release reservations errors: %v", errs)
	}
	return nil
}

// GetActiveByAccount returns all active reservations for an account.
func (m *Manager) GetActiveByAccount(accountID string) []*models.Reservation {
	all := m.store.ListReservations()
	var active []*models.Reservation

	for _, res := range all {
		if res.AccountID == accountID && res.IsActive() {
			active = append(active, res)
		}
	}

	return active
}

// GetTotalReservedPct returns the total reserved percentage for an account.
func (m *Manager) GetTotalReservedPct(accountID string) float64 {
	active := m.GetActiveByAccount(accountID)
	var total float64

	for _, res := range active {
		total += res.EstimatedCostPct
	}

	return total
}

// GetMetrics returns current metrics.
func (m *Manager) GetMetrics() Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	return *m.metrics
}

// StartCleanupRoutine starts a background goroutine that periodically cleans up expired reservations.
func (m *Manager) StartCleanupRoutine(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.CleanupExpired()
			}
		}
	}()
}
