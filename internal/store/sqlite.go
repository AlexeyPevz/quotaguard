package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/errors"
	"github.com/quotaguard/quotaguard/internal/logging"
	"github.com/quotaguard/quotaguard/internal/models"
	_ "modernc.org/sqlite"
)

// SQLiteStore provides a SQLite-based storage for quota information with WAL mode.
// It is thread-safe and supports concurrent access.
type SQLiteStore struct {
	mu       sync.RWMutex
	db       *sql.DB
	logger   *logging.Logger
	settings SettingsStore

	// Subscribers for quota changes
	subscribers map[string][]chan models.QuotaEvent
	subMu       sync.RWMutex

	// Retention cleanup
	cleanupTicker *time.Ticker
	cleanupDone   chan struct{}
	retentionDays int
}

// NewSQLiteStore creates a new SQLite store with WAL mode enabled
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	return NewSQLiteStoreWithRetention(dbPath, 30) // Default 30 days retention
}

// NewSQLiteStoreWithRetention creates a new SQLite store with custom retention
func NewSQLiteStoreWithRetention(dbPath string, retentionDays int) (*SQLiteStore, error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, &errors.ErrDirectoryCreate{Path: dir, Err: err}
		}
	}

	// Open database with WAL mode enabled
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_pragma=cache_size(2000)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, &errors.ErrDatabaseOpen{Path: dbPath, Err: err}
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, &errors.ErrDatabaseOpen{Path: dbPath, Err: err}
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	settingsStore, err := NewSQLiteSettingsStore(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	store := &SQLiteStore{
		db:            db,
		logger:        logging.NewLogger(),
		subscribers:   make(map[string][]chan models.QuotaEvent),
		cleanupDone:   make(chan struct{}),
		retentionDays: retentionDays,
		settings:      settingsStore,
	}

	// Start retention cleanup goroutine if retention is enabled
	if retentionDays > 0 {
		store.startCleanup()
	}

	return store, nil
}

// runMigrations runs database migrations
func runMigrations(db *sql.DB) error {
	// Create migrations table if not exists
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return &errors.ErrDatabaseQuery{Operation: "create migrations table", Err: err}
	}

	// Get current migration version
	var currentVersion int
	err = db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&currentVersion)
	if err != nil {
		return &errors.ErrDatabaseQuery{Operation: "get current migration version", Err: err}
	}

	// Define migrations
	migrations := []struct {
		version int
		up      string
	}{
		{
			version: 1,
			up: `
				CREATE TABLE IF NOT EXISTS accounts (
					id TEXT PRIMARY KEY,
					provider TEXT NOT NULL,
					enabled INTEGER NOT NULL DEFAULT 1,
					priority INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
				);

				CREATE TABLE IF NOT EXISTS quotas (
					account_id TEXT PRIMARY KEY,
					effective_remaining_pct REAL NOT NULL,
					is_throttled INTEGER NOT NULL DEFAULT 0,
					source TEXT NOT NULL,
					collected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					dimensions TEXT,
					FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
				);

				CREATE TABLE IF NOT EXISTS reservations (
					id TEXT PRIMARY KEY,
					account_id TEXT NOT NULL,
					correlation_id TEXT NOT NULL,
					estimated_cost_pct REAL NOT NULL,
					actual_cost_pct REAL,
					status TEXT NOT NULL,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					expires_at DATETIME NOT NULL,
					released_at DATETIME,
					FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
				);

				CREATE INDEX IF NOT EXISTS idx_accounts_provider ON accounts(provider);
				CREATE INDEX IF NOT EXISTS idx_accounts_enabled ON accounts(enabled);
				CREATE INDEX IF NOT EXISTS idx_quotas_collected_at ON quotas(collected_at);
				CREATE INDEX IF NOT EXISTS idx_reservations_account_id ON reservations(account_id);
				CREATE INDEX IF NOT EXISTS idx_reservations_status ON reservations(status);
				CREATE INDEX IF NOT EXISTS idx_reservations_created_at ON reservations(created_at);
			`,
		},
		{
			version: 2,
			up: `
				ALTER TABLE quotas ADD COLUMN virtual_used_pct REAL NOT NULL DEFAULT 0;
			`,
		},
		{
			version: 3,
			up: `
				ALTER TABLE accounts ADD COLUMN tier TEXT DEFAULT '';
				ALTER TABLE accounts ADD COLUMN concurrency_limit INTEGER NOT NULL DEFAULT 0;
				ALTER TABLE accounts ADD COLUMN input_cost REAL NOT NULL DEFAULT 0;
				ALTER TABLE accounts ADD COLUMN output_cost REAL NOT NULL DEFAULT 0;
				ALTER TABLE accounts ADD COLUMN credentials_ref TEXT DEFAULT '';
			`,
		},
		{
			version: 4,
			up: `
				ALTER TABLE accounts ADD COLUMN blocked_until DATETIME;

				CREATE TABLE IF NOT EXISTS account_credentials (
					account_id TEXT PRIMARY KEY,
					type TEXT NOT NULL,
					data TEXT NOT NULL,
					updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
				);
			`,
		},
	}

	// Run pending migrations
	tx, err := db.Begin()
	if err != nil {
		return &errors.ErrDatabaseQuery{Operation: "begin transaction", Err: err}
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, m := range migrations {
		if m.version > currentVersion {
			if _, err := tx.Exec(m.up); err != nil {
				return &errors.ErrDatabaseMigration{Version: m.version, Err: err}
			}
			if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
				return &errors.ErrDatabaseMigration{Version: m.version, Err: err}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return &errors.ErrDatabaseQuery{Operation: "commit migrations", Err: err}
	}

	return nil
}

// startCleanup starts the retention cleanup goroutine
func (s *SQLiteStore) startCleanup() {
	s.cleanupTicker = time.NewTicker(time.Hour)
	go func() {
		for {
			select {
			case <-s.cleanupTicker.C:
				s.cleanupOldData()
			case <-s.cleanupDone:
				return
			}
		}
	}()
}

// cleanupOldData removes old data based on retention policy
func (s *SQLiteStore) cleanupOldData() {
	if s.retentionDays <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)

	// Cleanup old quota data
	_, err := s.db.Exec("DELETE FROM quotas WHERE collected_at < ?", cutoff)
	if err != nil {
		s.logger.Error("cleanup failed", "table", "quotas", "error", err.Error())
	}

	// Cleanup old reservations (released or cancelled)
	_, err = s.db.Exec(`
		DELETE FROM reservations
		WHERE status IN ('released', 'cancelled')
		AND (released_at < ? OR created_at < ?)
	`, cutoff, cutoff)
	if err != nil {
		s.logger.Error("cleanup failed", "table", "reservations", "error", err.Error())
	}
}

// Close gracefully shuts down the store
func (s *SQLiteStore) Close() error {
	// Stop cleanup goroutine
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
		close(s.cleanupDone)
	}

	// Close all subscriber channels
	s.subMu.Lock()
	for _, subs := range s.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}
	s.subscribers = make(map[string][]chan models.QuotaEvent)
	s.subMu.Unlock()

	// Close database connection
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Settings returns the settings store.
func (s *SQLiteStore) Settings() SettingsStore {
	return s.settings
}

// Account operations

// GetAccount retrieves an account by ID
func (s *SQLiteStore) GetAccount(id string) (*models.Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var acc models.Account

	err := s.db.QueryRow(`
		SELECT id, provider, enabled, priority, tier, concurrency_limit, input_cost, output_cost, credentials_ref, blocked_until, created_at, updated_at
		FROM accounts WHERE id = ?
	`, id).Scan(&acc.ID, &acc.Provider, &acc.Enabled, &acc.Priority, &acc.Tier, &acc.ConcurrencyLimit, &acc.InputCost, &acc.OutputCost, &acc.CredentialsRef, &acc.BlockedUntil, &acc.CreatedAt, &acc.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		return nil, false
	}

	return &acc, true
}

// SetAccount stores or updates an account
func (s *SQLiteStore) SetAccount(acc *models.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO accounts (id, provider, enabled, priority, tier, concurrency_limit, input_cost, output_cost, credentials_ref, blocked_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider = excluded.provider,
			enabled = excluded.enabled,
			priority = excluded.priority,
			tier = excluded.tier,
			concurrency_limit = excluded.concurrency_limit,
			input_cost = excluded.input_cost,
			output_cost = excluded.output_cost,
			credentials_ref = excluded.credentials_ref,
			blocked_until = excluded.blocked_until,
			updated_at = excluded.updated_at
	`, acc.ID, acc.Provider, acc.Enabled, acc.Priority, acc.Tier, acc.ConcurrencyLimit, acc.InputCost, acc.OutputCost, acc.CredentialsRef, acc.BlockedUntil, now, now)

	if err != nil {
		s.logger.Error("failed to set account", "error", err.Error())
	}
}

// SetAccountBlockedUntil updates blocked_until for an account.
func (s *SQLiteStore) SetAccountBlockedUntil(id string, blockedUntil *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE accounts SET blocked_until = ?, updated_at = ? WHERE id = ?
	`, blockedUntil, time.Now(), id)
	return err
}

// DeleteAccount removes an account
func (s *SQLiteStore) DeleteAccount(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM accounts WHERE id = ?", id)
	if err != nil {
		return false
	}

	rows, _ := result.RowsAffected()
	return rows > 0
}

// ListAccounts returns all accounts
func (s *SQLiteStore) ListAccounts() []*models.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, provider, enabled, priority, tier, concurrency_limit, input_cost, output_cost, credentials_ref, blocked_until, created_at, updated_at
		FROM accounts ORDER BY priority DESC, id
	`)
	if err != nil {
		return []*models.Account{}
	}
	defer rows.Close()

	var accounts []*models.Account
	for rows.Next() {
		var acc models.Account

		if err := rows.Scan(&acc.ID, &acc.Provider, &acc.Enabled, &acc.Priority, &acc.Tier, &acc.ConcurrencyLimit, &acc.InputCost, &acc.OutputCost, &acc.CredentialsRef, &acc.BlockedUntil, &acc.CreatedAt, &acc.UpdatedAt); err != nil {
			continue
		}

		accounts = append(accounts, &acc)
	}

	return accounts
}

// ListEnabledAccounts returns only enabled accounts
func (s *SQLiteStore) ListEnabledAccounts() []*models.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, provider, enabled, priority, tier, concurrency_limit, input_cost, output_cost, credentials_ref, blocked_until, created_at, updated_at
		FROM accounts WHERE enabled = 1 ORDER BY priority DESC, id
	`)
	if err != nil {
		return []*models.Account{}
	}
	defer rows.Close()

	var accounts []*models.Account
	for rows.Next() {
		var acc models.Account

		if err := rows.Scan(&acc.ID, &acc.Provider, &acc.Enabled, &acc.Priority, &acc.Tier, &acc.ConcurrencyLimit, &acc.InputCost, &acc.OutputCost, &acc.CredentialsRef, &acc.BlockedUntil, &acc.CreatedAt, &acc.UpdatedAt); err != nil {
			continue
		}

		accounts = append(accounts, &acc)
	}

	return accounts
}

// Credentials operations

// GetAccountCredentials retrieves credentials for an account.
func (s *SQLiteStore) GetAccountCredentials(accountID string) (*models.AccountCredentials, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var credType string
	var data string
	var updatedAt time.Time
	err := s.db.QueryRow(`
		SELECT type, data, updated_at FROM account_credentials WHERE account_id = ?
	`, accountID).Scan(&credType, &data, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		return nil, false
	}

	var creds models.AccountCredentials
	if err := json.Unmarshal([]byte(data), &creds); err != nil {
		return nil, false
	}
	creds.AccountID = accountID
	creds.Type = credType
	creds.UpdatedAt = updatedAt
	return &creds, true
}

// SetAccountCredentials stores credentials for an account.
func (s *SQLiteStore) SetAccountCredentials(accountID string, creds *models.AccountCredentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if creds == nil {
		return nil
	}
	creds.AccountID = accountID
	creds.UpdatedAt = time.Now()
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO account_credentials (account_id, type, data, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			type = excluded.type,
			data = excluded.data,
			updated_at = excluded.updated_at
	`, accountID, creds.Type, string(data), creds.UpdatedAt)
	return err
}

// DeleteAccountCredentials removes credentials for an account.
func (s *SQLiteStore) DeleteAccountCredentials(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM account_credentials WHERE account_id = ?`, accountID)
	return err
}

// Quota operations

// GetQuota retrieves quota information for an account
func (s *SQLiteStore) GetQuota(accountID string) (*models.QuotaInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var quota models.QuotaInfo
	var dimensionsJSON sql.NullString

	err := s.db.QueryRow(`
		SELECT account_id, effective_remaining_pct, is_throttled, source, collected_at, dimensions, virtual_used_pct
		FROM quotas WHERE account_id = ?
	`, accountID).Scan(&quota.AccountID, &quota.EffectiveRemainingPct, &quota.IsThrottled, &quota.Source, &quota.CollectedAt, &dimensionsJSON, &quota.VirtualUsedPercent)

	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		return nil, false
	}

	if dimensionsJSON.Valid {
		if err := json.Unmarshal([]byte(dimensionsJSON.String), &quota.Dimensions); err != nil {
			s.logger.Warn("failed to parse quota dimensions", "error", err.Error(), "account_id", quota.AccountID)
		}
	}

	return &quota, true
}

// SetQuota stores or updates quota information
func (s *SQLiteStore) SetQuota(accountID string, quota *models.QuotaInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dimensionsJSON, _ := json.Marshal(quota.Dimensions)

	_, err := s.db.Exec(`
		INSERT INTO quotas (account_id, effective_remaining_pct, is_throttled, source, collected_at, dimensions, virtual_used_pct)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			effective_remaining_pct = excluded.effective_remaining_pct,
			is_throttled = excluded.is_throttled,
			source = excluded.source,
			collected_at = excluded.collected_at,
			dimensions = excluded.dimensions,
			virtual_used_pct = excluded.virtual_used_pct
	`, quota.AccountID, quota.EffectiveRemainingPct, quota.IsThrottled, quota.Source, quota.CollectedAt, dimensionsJSON, quota.VirtualUsedPercent)

	if err != nil {
		s.logger.Error("failed to set quota", "error", err.Error())
	}
}

// UpdateQuota updates quota information for an account
func (s *SQLiteStore) UpdateQuota(accountID string, quota *models.QuotaInfo) error {
	// Get old quota for notification BEFORE acquiring lock to avoid deadlock
	var oldQuota *models.QuotaInfo
	var oldDimensionsJSON sql.NullString

	// Use temporary variables for scanning
	var tempAccountID string
	var tempRemaining float64
	var tempThrottled bool
	var tempSource string
	var tempCollectedAt time.Time
	var tempVirtualUsed float64

	err := s.db.QueryRow(`
		SELECT account_id, effective_remaining_pct, is_throttled, source, collected_at, dimensions, virtual_used_pct
		FROM quotas WHERE account_id = ?
	`, accountID).Scan(&tempAccountID, &tempRemaining, &tempThrottled, &tempSource, &tempCollectedAt, &oldDimensionsJSON, &tempVirtualUsed)

	if err == sql.ErrNoRows {
		oldQuota = nil
	} else if err == nil {
		oldQuota = &models.QuotaInfo{
			AccountID:             tempAccountID,
			EffectiveRemainingPct: tempRemaining,
			IsThrottled:           tempThrottled,
			Source:                models.Source(tempSource),
			CollectedAt:           tempCollectedAt,
			VirtualUsedPercent:    tempVirtualUsed,
		}
		if oldDimensionsJSON.Valid {
			if err := json.Unmarshal([]byte(oldDimensionsJSON.String), &oldQuota.Dimensions); err != nil {
				s.logger.Warn("failed to parse quota dimensions", "error", err.Error(), "account_id", accountID)
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dimensionsJSON, _ := json.Marshal(quota.Dimensions)

	_, err = s.db.Exec(`
		INSERT INTO quotas (account_id, effective_remaining_pct, is_throttled, source, collected_at, dimensions, virtual_used_pct)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			effective_remaining_pct = excluded.effective_remaining_pct,
			is_throttled = excluded.is_throttled,
			source = excluded.source,
			collected_at = excluded.collected_at,
			dimensions = excluded.dimensions,
			virtual_used_pct = excluded.virtual_used_pct
	`, quota.AccountID, quota.EffectiveRemainingPct, quota.IsThrottled, quota.Source, quota.CollectedAt, dimensionsJSON, quota.VirtualUsedPercent)

	if err != nil {
		return &errors.ErrDatabaseQuery{Operation: "update quota", Err: err}
	}

	// Notify subscribers if dimensions changed
	if oldQuota != nil {
		s.notifyQuotaChange(accountID, oldQuota, quota)
	}

	return nil
}

// DeleteQuota removes quota information
func (s *SQLiteStore) DeleteQuota(accountID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM quotas WHERE account_id = ?", accountID)
	if err != nil {
		return false
	}

	rows, _ := result.RowsAffected()
	return rows > 0
}

// ListQuotas returns all quota information
func (s *SQLiteStore) ListQuotas() map[string]*models.QuotaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT account_id, effective_remaining_pct, is_throttled, source, collected_at, dimensions, virtual_used_pct
		FROM quotas
	`)
	if err != nil {
		return make(map[string]*models.QuotaInfo)
	}
	defer rows.Close()

	quotas := make(map[string]*models.QuotaInfo)
	for rows.Next() {
		var quota models.QuotaInfo
		var dimensionsJSON sql.NullString

		if err := rows.Scan(&quota.AccountID, &quota.EffectiveRemainingPct, &quota.IsThrottled, &quota.Source, &quota.CollectedAt, &dimensionsJSON, &quota.VirtualUsedPercent); err != nil {
			continue
		}

		if dimensionsJSON.Valid {
			if err := json.Unmarshal([]byte(dimensionsJSON.String), &quota.Dimensions); err != nil {
				s.logger.Warn("failed to parse quota dimensions", "error", err.Error(), "account_id", quota.AccountID)
			}
		}

		quotas[quota.AccountID] = &quota
	}

	return quotas
}

// Subscribe creates a subscription for quota changes on an account
func (s *SQLiteStore) Subscribe(accountID string) chan models.QuotaEvent {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	ch := make(chan models.QuotaEvent, 10)
	s.subscribers[accountID] = append(s.subscribers[accountID], ch)
	return ch
}

// Unsubscribe removes a subscription
func (s *SQLiteStore) Unsubscribe(accountID string, ch chan models.QuotaEvent) {
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
func (s *SQLiteStore) notifyQuotaChange(accountID string, oldQuota, newQuota *models.QuotaInfo) {
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
func (s *SQLiteStore) GetReservation(id string) (*models.Reservation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var res models.Reservation
	var actualCostPct sql.NullFloat64
	var releasedAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, account_id, correlation_id, estimated_cost_pct, actual_cost_pct,
		       status, created_at, expires_at, released_at
		FROM reservations WHERE id = ?
	`, id).Scan(&res.ID, &res.AccountID, &res.CorrelationID, &res.EstimatedCostPct,
		&actualCostPct, &res.Status, &res.CreatedAt, &res.ExpiresAt, &releasedAt)

	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		return nil, false
	}

	if actualCostPct.Valid {
		val := actualCostPct.Float64
		res.ActualCostPct = &val
	}
	if releasedAt.Valid {
		val := releasedAt.Time
		res.ReleasedAt = &val
	}

	return &res, true
}

// SetReservation stores or updates a reservation
func (s *SQLiteStore) SetReservation(id string, res *models.Reservation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var actualCostPct interface{} = nil
	if res.ActualCostPct != nil && *res.ActualCostPct > 0 {
		actualCostPct = *res.ActualCostPct
	}

	var releasedAt interface{} = nil
	if res.ReleasedAt != nil && !res.ReleasedAt.IsZero() {
		releasedAt = *res.ReleasedAt
	}

	_, err := s.db.Exec(`
		INSERT INTO reservations (id, account_id, correlation_id, estimated_cost_pct, actual_cost_pct,
		                         status, created_at, expires_at, released_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id = excluded.account_id,
			correlation_id = excluded.correlation_id,
			estimated_cost_pct = excluded.estimated_cost_pct,
			actual_cost_pct = excluded.actual_cost_pct,
			status = excluded.status,
			expires_at = excluded.expires_at,
			released_at = excluded.released_at
	`, res.ID, res.AccountID, res.CorrelationID, res.EstimatedCostPct, actualCostPct,
		res.Status, res.CreatedAt, res.ExpiresAt, releasedAt)

	if err != nil {
		s.logger.Error("failed to set reservation", "error", err.Error())
	}
}

// DeleteReservation removes a reservation
func (s *SQLiteStore) DeleteReservation(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM reservations WHERE id = ?", id)
	if err != nil {
		return false
	}

	rows, _ := result.RowsAffected()
	return rows > 0
}

// ListReservations returns all reservations
func (s *SQLiteStore) ListReservations() []*models.Reservation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, account_id, correlation_id, estimated_cost_pct, actual_cost_pct,
		       status, created_at, expires_at, released_at
		FROM reservations ORDER BY created_at DESC
	`)
	if err != nil {
		return []*models.Reservation{}
	}
	defer rows.Close()

	var reservations []*models.Reservation
	for rows.Next() {
		var res models.Reservation
		var actualCostPct sql.NullFloat64
		var releasedAt sql.NullTime

		if err := rows.Scan(&res.ID, &res.AccountID, &res.CorrelationID, &res.EstimatedCostPct,
			&actualCostPct, &res.Status, &res.CreatedAt, &res.ExpiresAt, &releasedAt); err != nil {
			continue
		}

		if actualCostPct.Valid {
			val := actualCostPct.Float64
			res.ActualCostPct = &val
		}
		if releasedAt.Valid {
			val := releasedAt.Time
			res.ReleasedAt = &val
		}

		reservations = append(reservations, &res)
	}

	return reservations
}

// Clear removes all data from the store
func (s *SQLiteStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec("DELETE FROM reservations"); err != nil {
		s.logger.Error("failed to clear reservations", "error", err.Error())
	}
	if _, err := s.db.Exec("DELETE FROM quotas"); err != nil {
		s.logger.Error("failed to clear quotas", "error", err.Error())
	}
	if _, err := s.db.Exec("DELETE FROM accounts"); err != nil {
		s.logger.Error("failed to clear accounts", "error", err.Error())
	}
}

// Stats returns statistics about the store
func (s *SQLiteStore) Stats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var accountCount, quotaCount, reservationCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&accountCount); err != nil {
		s.logger.Error("failed to count accounts", "error", err.Error())
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM quotas").Scan(&quotaCount); err != nil {
		s.logger.Error("failed to count quotas", "error", err.Error())
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM reservations").Scan(&reservationCount); err != nil {
		s.logger.Error("failed to count reservations", "error", err.Error())
	}

	return StoreStats{
		AccountCount:     accountCount,
		QuotaCount:       quotaCount,
		ReservationCount: reservationCount,
	}
}

// Ensure SQLiteStore implements the Store interface
var _ Store = (*SQLiteStore)(nil)
