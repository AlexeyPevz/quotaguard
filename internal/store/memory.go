package store

import (
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// MemoryStore provides an in-memory storage for quota information and accounts.
// It is thread-safe and supports concurrent access.
type MemoryStore struct {
	mu           sync.RWMutex
	quotas       map[string]*models.QuotaInfo // key: accountID
	accounts     map[string]*models.Account   // key: accountID
	credentials  map[string]*models.AccountCredentials
	reservations map[string]*models.Reservation // key: reservationID
	settings     SettingsStore

	// Subscribers for quota changes
	subscribers map[string][]chan models.QuotaEvent
	subMu       sync.RWMutex
}

// NewMemoryStore creates a new in-memory store
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		quotas:       make(map[string]*models.QuotaInfo),
		accounts:     make(map[string]*models.Account),
		credentials:  make(map[string]*models.AccountCredentials),
		reservations: make(map[string]*models.Reservation),
		subscribers:  make(map[string][]chan models.QuotaEvent),
		settings:     NewMemorySettingsStore(),
	}
}

// Account operations

// GetAccount retrieves an account by ID
func (s *MemoryStore) GetAccount(id string) (*models.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	acc, ok := s.accounts[id]
	if !ok {
		return nil, false
	}
	return acc, true
}

// SetAccount stores or updates an account
func (s *MemoryStore) SetAccount(acc *models.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.accounts[acc.ID] = acc
}

// SetAccountBlockedUntil updates blocked_until for an account.
func (s *MemoryStore) SetAccountBlockedUntil(id string, blockedUntil *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return nil
	}
	acc.BlockedUntil = blockedUntil
	s.accounts[id] = acc
	return nil
}

// DeleteAccount removes an account
func (s *MemoryStore) DeleteAccount(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.accounts[id]; !ok {
		return false
	}
	delete(s.accounts, id)
	delete(s.quotas, id)
	delete(s.credentials, id)
	return true
}

// ListAccounts returns all accounts
func (s *MemoryStore) ListAccounts() []*models.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*models.Account, 0, len(s.accounts))
	for _, acc := range s.accounts {
		result = append(result, acc)
	}
	return result
}

// Credentials operations
func (s *MemoryStore) GetAccountCredentials(accountID string) (*models.AccountCredentials, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, ok := s.credentials[accountID]
	if !ok {
		return nil, false
	}
	return creds, true
}

func (s *MemoryStore) SetAccountCredentials(accountID string, creds *models.AccountCredentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if creds != nil {
		creds.AccountID = accountID
	}
	s.credentials[accountID] = creds
	return nil
}

func (s *MemoryStore) DeleteAccountCredentials(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.credentials, accountID)
	return nil
}

// ListEnabledAccounts returns only enabled accounts
func (s *MemoryStore) ListEnabledAccounts() []*models.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*models.Account, 0)
	for _, acc := range s.accounts {
		if acc.Enabled {
			result = append(result, acc)
		}
	}
	return result
}

// Quota operations

// GetQuota retrieves quota information for an account
func (s *MemoryStore) GetQuota(accountID string) (*models.QuotaInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	quota, ok := s.quotas[accountID]
	if !ok {
		return nil, false
	}
	return quota, true
}

// SetQuota stores or updates quota information
func (s *MemoryStore) SetQuota(accountID string, quota *models.QuotaInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.quotas[accountID] = quota
}

// UpdateQuota updates quota information for an account
func (s *MemoryStore) UpdateQuota(accountID string, quota *models.QuotaInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldQuota, ok := s.quotas[accountID]
	s.quotas[accountID] = quota

	// Notify subscribers if dimensions changed
	if ok && oldQuota != nil {
		s.notifyQuotaChange(accountID, oldQuota, quota)
	}

	return nil
}

// DeleteQuota removes quota information
func (s *MemoryStore) DeleteQuota(accountID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.quotas[accountID]; !ok {
		return false
	}
	delete(s.quotas, accountID)
	return true
}

// ListQuotas returns all quota information
func (s *MemoryStore) ListQuotas() map[string]*models.QuotaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*models.QuotaInfo, len(s.quotas))
	for k, v := range s.quotas {
		result[k] = v
	}
	return result
}

// Subscribe creates a subscription for quota changes on an account
func (s *MemoryStore) Subscribe(accountID string) chan models.QuotaEvent {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	ch := make(chan models.QuotaEvent, 10)
	s.subscribers[accountID] = append(s.subscribers[accountID], ch)
	return ch
}

// Unsubscribe removes a subscription
func (s *MemoryStore) Unsubscribe(accountID string, ch chan models.QuotaEvent) {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	subs := s.subscribers[accountID]
	for i, sub := range subs {
		if sub == ch {
			// Remove by swapping with last and truncating
			subs[i] = subs[len(subs)-1]
			s.subscribers[accountID] = subs[:len(subs)-1]
			close(ch)
			return
		}
	}
}

// notifyQuotaChange sends events to subscribers when quota changes
func (s *MemoryStore) notifyQuotaChange(accountID string, oldQuota, newQuota *models.QuotaInfo) {
	s.subMu.RLock()
	subs := s.subscribers[accountID]
	s.subMu.RUnlock()

	if len(subs) == 0 {
		return
	}

	// Check for changes in effective remaining percentage
	if oldQuota.EffectiveRemainingPct != newQuota.EffectiveRemainingPct {
		event := models.QuotaEvent{
			AccountID: accountID,
			Dimension: models.Dimension{}, // Empty dimension for overall change
			OldValue:  int64(oldQuota.EffectiveRemainingPct * 100),
			NewValue:  int64(newQuota.EffectiveRemainingPct * 100),
			Timestamp: time.Now(),
		}

		for _, ch := range subs {
			select {
			case ch <- event:
			default:
				// Channel full, skip
			}
		}
	}
}

// Reservation operations

// GetReservation retrieves a reservation by ID
func (s *MemoryStore) GetReservation(id string) (*models.Reservation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res, ok := s.reservations[id]
	if !ok {
		return nil, false
	}
	return res, true
}

// SetReservation stores or updates a reservation
func (s *MemoryStore) SetReservation(id string, res *models.Reservation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reservations[id] = res
}

// DeleteReservation removes a reservation
func (s *MemoryStore) DeleteReservation(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.reservations[id]; !ok {
		return false
	}
	delete(s.reservations, id)
	return true
}

// ListReservations returns all reservations
func (s *MemoryStore) ListReservations() []*models.Reservation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*models.Reservation, 0, len(s.reservations))
	for _, res := range s.reservations {
		result = append(result, res)
	}
	return result
}

// Clear removes all data from the store
func (s *MemoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.quotas = make(map[string]*models.QuotaInfo)
	s.accounts = make(map[string]*models.Account)
	s.reservations = make(map[string]*models.Reservation)
	if settings, ok := s.settings.(*MemorySettingsStore); ok {
		settings.Clear()
	}
}

// Stats returns statistics about the store
func (s *MemoryStore) Stats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StoreStats{
		AccountCount:     len(s.accounts),
		QuotaCount:       len(s.quotas),
		ReservationCount: len(s.reservations),
	}
}

// Settings returns the settings store.
func (s *MemoryStore) Settings() SettingsStore {
	return s.settings
}

// Close implements Store Close (no-op for memory store).
func (s *MemoryStore) Close() error {
	return nil
}

// StoreStats contains statistics about the store
type StoreStats struct {
	AccountCount     int
	QuotaCount       int
	ReservationCount int
}

// Ensure MemoryStore implements the Store interface
var _ Store = (*MemoryStore)(nil)

// Store defines the interface for quota storage
type Store interface {
	// Account operations
	GetAccount(id string) (*models.Account, bool)
	SetAccount(acc *models.Account)
	SetAccountBlockedUntil(id string, blockedUntil *time.Time) error
	DeleteAccount(id string) bool
	ListAccounts() []*models.Account
	ListEnabledAccounts() []*models.Account

	// Credentials operations
	GetAccountCredentials(accountID string) (*models.AccountCredentials, bool)
	SetAccountCredentials(accountID string, creds *models.AccountCredentials) error
	DeleteAccountCredentials(accountID string) error

	// Quota operations
	GetQuota(accountID string) (*models.QuotaInfo, bool)
	SetQuota(accountID string, quota *models.QuotaInfo)
	UpdateQuota(accountID string, quota *models.QuotaInfo) error
	DeleteQuota(accountID string) bool
	ListQuotas() map[string]*models.QuotaInfo

	// Reservation operations
	GetReservation(id string) (*models.Reservation, bool)
	SetReservation(id string, res *models.Reservation)
	DeleteReservation(id string) bool
	ListReservations() []*models.Reservation

	// Subscription
	Subscribe(accountID string) chan models.QuotaEvent
	Unsubscribe(accountID string, ch chan models.QuotaEvent)

	// Management
	Clear()
	Stats() StoreStats
	Settings() SettingsStore
	Close() error
}
