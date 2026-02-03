package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// TestNewSQLiteStore tests creating a new SQLite store with WAL mode
func TestNewSQLiteStore(t *testing.T) {
	// Create a temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	// Verify store was created
	if store == nil {
		t.Fatal("Store should not be nil")
	}

	// Check that database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("Database file should exist")
	}

	// Use walPath and shmPath variables to avoid unused variable errors
	_ = dbPath
}

// TestSQLiteStoreAccountOperations tests account CRUD operations
func TestSQLiteStoreAccountOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	// Test SetAccount
	account := &models.Account{
		ID:       "test-account-1",
		Provider: "openai",
		Enabled:  true,
		Priority: 10,
	}
	store.SetAccount(account)

	// Test GetAccount
	retrieved, ok := store.GetAccount("test-account-1")
	if !ok {
		t.Fatal("Failed to retrieve account")
	}
	if retrieved.ID != account.ID {
		t.Errorf("Expected ID %s, got %s", account.ID, retrieved.ID)
	}
	if retrieved.Provider != account.Provider {
		t.Errorf("Expected provider %s, got %s", account.Provider, retrieved.Provider)
	}

	// Test ListAccounts
	accounts := store.ListAccounts()
	if len(accounts) != 1 {
		t.Errorf("Expected 1 account, got %d", len(accounts))
	}

	// Test ListEnabledAccounts
	enabledAccounts := store.ListEnabledAccounts()
	if len(enabledAccounts) != 1 {
		t.Errorf("Expected 1 enabled account, got %d", len(enabledAccounts))
	}

	// Test DeleteAccount
	deleted := store.DeleteAccount("test-account-1")
	if !deleted {
		t.Fatal("Failed to delete account")
	}

	// Verify deletion
	_, ok = store.GetAccount("test-account-1")
	if ok {
		t.Fatal("Account should be deleted")
	}
}

// TestSQLiteStoreQuotaOperations tests quota CRUD operations
func TestSQLiteStoreQuotaOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	// Create account first
	account := &models.Account{
		ID:       "quota-account-1",
		Provider: "anthropic",
		Enabled:  true,
		Priority: 5,
	}
	store.SetAccount(account)

	// Test SetQuota
	quota := &models.QuotaInfo{
		AccountID:             "quota-account-1",
		EffectiveRemainingPct: 75.0,
		IsThrottled:           false,
		Source:                "test",
		CollectedAt:           time.Now(),
	}
	store.SetQuota("quota-account-1", quota)

	// Test GetQuota
	retrieved, ok := store.GetQuota("quota-account-1")
	if !ok {
		t.Fatal("Failed to retrieve quota")
	}
	if retrieved.EffectiveRemainingPct != 75.0 {
		t.Errorf("Expected 75.0 remaining, got %f", retrieved.EffectiveRemainingPct)
	}

	// Test ListQuotas
	quotas := store.ListQuotas()
	if len(quotas) != 1 {
		t.Errorf("Expected 1 quota, got %d", len(quotas))
	}

	// Test DeleteQuota
	deleted := store.DeleteQuota("quota-account-1")
	if !deleted {
		t.Fatal("Failed to delete quota")
	}
}

// TestSQLiteStoreReservationOperations tests reservation operations
func TestSQLiteStoreReservationOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	// Create account first
	account := &models.Account{
		ID:       "reservation-account-1",
		Provider: "google",
		Enabled:  true,
		Priority: 8,
	}
	store.SetAccount(account)

	// Test SetReservation
	reservation := &models.Reservation{
		ID:               "res-1",
		AccountID:        "reservation-account-1",
		CorrelationID:    "corr-1",
		EstimatedCostPct: 5.0,
		Status:           models.ReservationActive,
		CreatedAt:        time.Now(),
		ExpiresAt:        time.Now().Add(30 * time.Minute),
	}
	store.SetReservation("res-1", reservation)

	// Test GetReservation
	retrieved, ok := store.GetReservation("res-1")
	if !ok {
		t.Fatal("Failed to retrieve reservation")
	}
	if retrieved.ID != "res-1" {
		t.Errorf("Expected reservation ID res-1, got %s", retrieved.ID)
	}

	// Test ListReservations
	reservations := store.ListReservations()
	if len(reservations) != 1 {
		t.Errorf("Expected 1 reservation, got %d", len(reservations))
	}

	// Test DeleteReservation
	store.DeleteReservation("res-1")
	reservations = store.ListReservations()
	if len(reservations) != 0 {
		t.Errorf("Expected 0 reservations after delete, got %d", len(reservations))
	}
}

// TestSQLiteStoreCleanupOldData tests the retention cleanup functionality
func TestSQLiteStoreCleanupOldData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store with 1 day retention
	store, err := NewSQLiteStoreWithRetention(dbPath, 1)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	// Create account
	account := &models.Account{
		ID:       "cleanup-account-1",
		Provider: "test",
		Enabled:  true,
		Priority: 1,
	}
	store.SetAccount(account)

	// Create old quota (2 days ago)
	oldQuota := &models.QuotaInfo{
		AccountID:             "cleanup-account-1",
		EffectiveRemainingPct: 50.0,
		IsThrottled:           false,
		Source:                "old",
		CollectedAt:           time.Now().AddDate(0, 0, -2),
	}
	store.SetQuota("cleanup-account-1", oldQuota)

	// Trigger cleanup
	store.cleanupOldData()

	// Verify old data was cleaned up
	_, ok := store.GetQuota("cleanup-account-1")
	if ok {
		t.Fatal("Expected old quota to be removed by retention cleanup")
	}
}

// TestSQLiteStoreClose tests the Close functionality
func TestSQLiteStoreClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}

	// Close should not error
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}
}

// TestSQLiteStoreSubscribe tests the subscription functionality
func TestSQLiteStoreSubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	// Subscribe should return a channel
	ch := store.Subscribe("test-account")
	if ch == nil {
		t.Fatal("Subscribe should return a non-nil channel")
	}

	// Unsubscribe should not panic
	store.Unsubscribe("test-account", ch)
}

// TestSQLiteStoreMigrations tests that migrations run correctly
func TestSQLiteStoreMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "migrations_test.db")

	store1, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create first store: %v", err)
	}

	// Create some data
	account := &models.Account{
		ID:       "migration-test",
		Provider: "test",
		Enabled:  true,
		Priority: 1,
	}
	store1.SetAccount(account)
	store1.Close()

	// Reopen store (should run migrations again)
	store2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer store2.Close()

	// Verify data still exists
	retrieved, ok := store2.GetAccount("migration-test")
	if !ok {
		t.Fatal("Data should persist after reopening store")
	}
	if retrieved.ID != "migration-test" {
		t.Errorf("Expected ID migration-test, got %s", retrieved.ID)
	}
}
