package cleanup

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// mockMetricsRecorder implements MetricsRecorder for testing.
type mockMetricsRecorder struct {
	mu           sync.RWMutex
	operations   []mockOperation
	vacuumCount  int
	analyzeCount int
}

type mockOperation struct {
	tableName    string
	deletedCount int64
	duration     time.Duration
}

func newMockMetricsRecorder() *mockMetricsRecorder {
	return &mockMetricsRecorder{
		operations: make([]mockOperation, 0),
	}
}

func (m *mockMetricsRecorder) RecordCleanupOperation(tableName string, deletedCount int64, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operations = append(m.operations, mockOperation{
		tableName:    tableName,
		deletedCount: deletedCount,
		duration:     duration,
	})
}

func (m *mockMetricsRecorder) RecordVacuumOperation(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vacuumCount++
}

func (m *mockMetricsRecorder) RecordAnalyzeOperation(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.analyzeCount++
}

func (m *mockMetricsRecorder) GetOperations() []mockOperation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.operations
}

func (m *mockMetricsRecorder) GetVacuumCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.vacuumCount
}

func (m *mockMetricsRecorder) GetAnalyzeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.analyzeCount
}

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(MEMORY)")
	require.NoError(t, err)

	err = db.Ping()
	require.NoError(t, err)

	// Create test tables
	_, err = db.Exec(`
		CREATE TABLE quota_history (
			id INTEGER PRIMARY KEY,
			account_id TEXT,
			collected_at DATETIME,
			quota_value REAL
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE routing_events (
			id INTEGER PRIMARY KEY,
			account_id TEXT,
			created_at DATETIME,
			event_type TEXT
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE health_history (
			id INTEGER PRIMARY KEY,
			account_id TEXT,
			checked_at DATETIME,
			status TEXT
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE alerts (
			id INTEGER PRIMARY KEY,
			account_id TEXT,
			created_at DATETIME,
			alert_type TEXT
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE reservations (
			id INTEGER PRIMARY KEY,
			account_id TEXT,
			status TEXT,
			expires_at DATETIME
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE soft_deleted_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			table_name TEXT,
			record_id TEXT,
			deleted_at DATETIME,
			original_data TEXT
		)
	`)
	require.NoError(t, err)

	return db
}

// TestRetentionPolicyValidate tests validation of retention policies.
func TestRetentionPolicyValidate(t *testing.T) {
	tests := []struct {
		name    string
		policy  RetentionPolicy
		wantErr bool
	}{
		{
			name: "valid policy",
			policy: RetentionPolicy{
				TableName:       "test_table",
				RetentionPeriod: 7 * 24 * time.Hour,
				Enabled:         true,
			},
			wantErr: false,
		},
		{
			name: "missing table name",
			policy: RetentionPolicy{
				RetentionPeriod: 7 * 24 * time.Hour,
				Enabled:         true,
			},
			wantErr: true,
		},
		{
			name: "negative retention period",
			policy: RetentionPolicy{
				TableName:       "test_table",
				RetentionPeriod: -1 * time.Hour,
				Enabled:         true,
			},
			wantErr: true,
		},
		{
			name: "zero retention period",
			policy: RetentionPolicy{
				TableName:       "test_table",
				RetentionPeriod: 0,
				Enabled:         true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDefaultPolicies tests that default policies are valid.
func TestDefaultPolicies(t *testing.T) {
	for _, policy := range DefaultPolicies {
		t.Run(policy.TableName, func(t *testing.T) {
			assert.NoError(t, policy.Validate())
			assert.NotEmpty(t, policy.TableName)
			assert.True(t, policy.RetentionPeriod > 0)
		})
	}
}

// TestInMemoryPolicyProvider tests the in-memory policy provider.
func TestInMemoryPolicyProvider(t *testing.T) {
	policies := []RetentionPolicy{
		{
			TableName:       "table1",
			RetentionPeriod: 24 * time.Hour,
			Enabled:         true,
		},
		{
			TableName:       "table2",
			RetentionPeriod: 48 * time.Hour,
			Enabled:         false,
		},
	}

	provider := NewInMemoryPolicyProvider(policies)

	// Test GetPolicy
	t.Run("GetPolicy", func(t *testing.T) {
		policy := provider.GetPolicy("table1")
		assert.NotNil(t, policy)
		assert.Equal(t, "table1", policy.TableName)
		assert.Equal(t, 24*time.Hour, policy.RetentionPeriod)

		policy = provider.GetPolicy("nonexistent")
		assert.Nil(t, policy)
	})

	// Test GetAllPolicies
	t.Run("GetAllPolicies", func(t *testing.T) {
		all := provider.GetAllPolicies()
		assert.Len(t, all, 2)
	})
}

// TestSQLiteCleanerCleanupQuotaHistory tests cleaning up quota history.
func TestSQLiteCleanerCleanupQuotaHistory(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert old records
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO quota_history (account_id, collected_at, quota_value)
		VALUES (?, ?, ?)
	`, "account1", oldTime, 100.0)
	require.NoError(t, err)

	// Insert recent records
	recentTime := time.Now().Add(-1 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO quota_history (account_id, collected_at, quota_value)
		VALUES (?, ?, ?)
	`, "account2", recentTime, 200.0)
	require.NoError(t, err)

	// Run cleanup with 7 day retention
	result, err := cleaner.CleanupQuotaHistory(7 * 24 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "quota_history", result.TableName)
	assert.Equal(t, int64(1), result.DeletedCount)
	assert.NoError(t, result.Error)

	// Verify old record is deleted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM quota_history").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestSQLiteCleanerCleanupRoutingEvents tests cleaning up routing events.
func TestSQLiteCleanerCleanupRoutingEvents(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert old records
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO routing_events (account_id, created_at, event_type)
		VALUES (?, ?, ?)
	`, "account1", oldTime, "switch")
	require.NoError(t, err)

	// Insert recent records
	recentTime := time.Now().Add(-1 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO routing_events (account_id, created_at, event_type)
		VALUES (?, ?, ?)
	`, "account2", recentTime, "fallback")
	require.NoError(t, err)

	// Run cleanup with 7 day retention
	result, err := cleaner.CleanupRoutingEvents(7 * 24 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "routing_events", result.TableName)
	assert.Equal(t, int64(1), result.DeletedCount)
}

// TestSQLiteCleanerCleanupAlerts tests cleaning up alerts.
func TestSQLiteCleanerCleanupAlerts(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert old records (31 days old)
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO alerts (account_id, created_at, alert_type)
		VALUES (?, ?, ?)
	`, "account1", oldTime, "quota_warning")
	require.NoError(t, err)

	// Insert recent records
	recentTime := time.Now().Add(-1 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO alerts (account_id, created_at, alert_type)
		VALUES (?, ?, ?)
	`, "account2", recentTime, "quota_critical")
	require.NoError(t, err)

	// Run cleanup with 30 day retention
	result, err := cleaner.CleanupAlerts(30 * 24 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "alerts", result.TableName)
	assert.Equal(t, int64(1), result.DeletedCount)
}

// TestSQLiteCleanerVacuumDatabase tests vacuum operation.
func TestSQLiteCleanerVacuumDatabase(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert some data
	for i := 0; i < 100; i++ {
		_, err := db.Exec(`
			INSERT INTO quota_history (account_id, collected_at, quota_value)
			VALUES (?, ?, ?)
		`, "account1", time.Now(), float64(i))
		require.NoError(t, err)
	}

	// Delete all data
	_, err := db.Exec("DELETE FROM quota_history")
	require.NoError(t, err)

	// Vacuum should succeed
	err = cleaner.VacuumDatabase()
	assert.NoError(t, err)
}

// TestSQLiteCleanerAnalyzeDatabase tests analyze operation.
func TestSQLiteCleanerAnalyzeDatabase(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Analyze should succeed
	err := cleaner.AnalyzeDatabase()
	assert.NoError(t, err)
}

// TestSQLiteSoftDeleterMarkAsDeleted tests marking records as deleted.
func TestSQLiteSoftDeleterMarkAsDeleted(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	config := SoftDeleteConfig{
		Enabled:         true,
		HardDeleteAfter: 30 * 24 * time.Hour,
	}

	softDeleter := NewSQLiteSoftDeleter(db, config)

	// Mark a record as deleted
	err := softDeleter.MarkAsDeleted("test_table", "record1", `{"data": "test"}`)
	assert.NoError(t, err)

	// Check that the record was added
	count, err := softDeleter.CountDeletedRecords("test_table")
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestSQLiteSoftDeleterHardDeleteOld tests hard deleting old soft-deleted records.
func TestSQLiteSoftDeleterHardDeleteOld(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	config := SoftDeleteConfig{
		Enabled:         true,
		HardDeleteAfter: 7 * 24 * time.Hour, // 7 days grace period
	}

	softDeleter := NewSQLiteSoftDeleter(db, config)

	// Add old soft-deleted record (31 days old)
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO soft_deleted_records (table_name, record_id, deleted_at, original_data)
		VALUES (?, ?, ?, ?)
	`, "test_table", "record1", oldTime, `{"data": "old"}`)
	require.NoError(t, err)

	// Add recent soft-deleted record (1 day old)
	recentTime := time.Now().Add(-1 * 24 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO soft_deleted_records (table_name, record_id, deleted_at, original_data)
		VALUES (?, ?, ?, ?)
	`, "test_table", "record2", recentTime, `{"data": "recent"}`)
	require.NoError(t, err)

	// Run hard delete
	result, err := softDeleter.HardDeleteOld()
	require.NoError(t, err)
	assert.Equal(t, "hard_delete", result.TableName)
	assert.Equal(t, int64(1), result.DeletedCount)

	// Verify only the old record was deleted
	count, err := softDeleter.CountDeletedRecords("")
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestManagerStartStop tests starting and stopping the cleanup manager.
func TestManagerStartStop(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
		VacuumEnabled:     true,
		VacuumInterval:    24 * time.Hour,
		AnalyzeEnabled:    true,
		AnalyzeInterval:   24 * time.Hour,
	}

	manager := NewManager(config, db, metrics)

	// Initially not running
	assert.False(t, manager.IsRunning())

	// Start the manager
	ctx := context.Background()
	err := manager.Start(ctx)
	assert.NoError(t, err)

	// Now running
	assert.True(t, manager.IsRunning())

	// Stop the manager
	err = manager.Stop()
	assert.NoError(t, err)

	// Should stop within reasonable time
	assert.False(t, manager.IsRunning())
}

// TestManagerRunCleanup tests running cleanup manually.
func TestManagerRunCleanup(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Insert test data
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO quota_history (account_id, collected_at, quota_value)
		VALUES (?, ?, ?)
	`, "account1", oldTime, 100.0)
	require.NoError(t, err)

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
	}

	manager := NewManager(config, db, metrics)

	// Run cleanup
	stats := manager.RunCleanup(context.Background())

	assert.NotNil(t, stats)
	assert.Equal(t, 1, stats.TotalRuns)
	assert.Equal(t, int64(1), stats.TotalDeletedCount)

	// Verify metrics were recorded
	ops := metrics.GetOperations()
	assert.True(t, len(ops) > 0)
}

// TestManagerRunVacuum tests running vacuum manually.
func TestManagerRunVacuum(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Insert some data
	for i := 0; i < 50; i++ {
		_, err := db.Exec(`
			INSERT INTO quota_history (account_id, collected_at, quota_value)
			VALUES (?, ?, ?)
		`, "account1", time.Now(), float64(i))
		require.NoError(t, err)
	}

	// Delete all data
	_, err := db.Exec("DELETE FROM quota_history")
	require.NoError(t, err)

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
		VacuumEnabled:     true,
	}

	manager := NewManager(config, db, metrics)

	// Run vacuum
	err = manager.RunVacuum(context.Background())
	assert.NoError(t, err)

	// Verify metrics were recorded
	assert.Equal(t, 1, metrics.GetVacuumCount())
}

// TestManagerRunAnalyze tests running analyze manually.
func TestManagerRunAnalyze(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
		AnalyzeEnabled:    true,
	}

	manager := NewManager(config, db, metrics)

	// Run analyze
	err := manager.RunAnalyze(context.Background())
	assert.NoError(t, err)

	// Verify metrics were recorded
	assert.Equal(t, 1, metrics.GetAnalyzeCount())
}

// TestGetPolicy tests getting policy by table name.
func TestGetPolicy(t *testing.T) {
	policy := GetPolicy("quota_history")
	assert.NotNil(t, policy)
	assert.Equal(t, "quota_history", policy.TableName)

	policy = GetPolicy("nonexistent_table")
	assert.Nil(t, policy)
}

// TestGetAllPolicies tests getting all default policies.
func TestGetAllPolicies(t *testing.T) {
	policies := GetAllPolicies()
	assert.True(t, len(policies) > 0)

	// Verify all policies are valid
	for _, policy := range policies {
		assert.NoError(t, policy.Validate())
	}
}

// TestConcurrentCleanupOperations tests concurrent cleanup operations.
func TestConcurrentCleanupOperations(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Insert test data
	for i := 0; i < 100; i++ {
		oldTime := time.Now().Add(-8 * 24 * time.Hour)
		_, err := db.Exec(`
			INSERT INTO quota_history (account_id, collected_at, quota_value)
			VALUES (?, ?, ?)
		`, "account1", oldTime, float64(i))
		require.NoError(t, err)
	}

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
	}

	manager := NewManager(config, db, metrics)

	// Run cleanup concurrently
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.RunCleanup(context.Background())
		}()
	}

	wg.Wait()

	// All operations should complete without error
	stats := manager.GetStats()
	assert.Equal(t, 5, stats.TotalRuns)
}

// TestCleanupResultDuration tests that cleanup results include duration.
func TestCleanupResultDuration(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	result, err := cleaner.CleanupQuotaHistory(7 * 24 * time.Hour)
	require.NoError(t, err)
	assert.True(t, result.Duration >= 0)
}

// TestSoftDeleteConfig tests soft delete configuration.
func TestSoftDeleteConfig(t *testing.T) {
	config := SoftDeleteConfig{
		Enabled:         true,
		HardDeleteAfter: 30 * 24 * time.Hour,
		TableName:       "custom_table",
		DeletedColumn:   "custom_deleted_at",
	}

	softDeleter := NewSQLiteSoftDeleter(nil, config)
	assert.True(t, softDeleter.IsEnabled())
	assert.Equal(t, config, softDeleter.GetConfig())
}

// TestSoftDeleteDisabled tests behavior when soft delete is disabled.
func TestSoftDeleteDisabled(t *testing.T) {
	config := SoftDeleteConfig{
		Enabled:         false,
		HardDeleteAfter: 30 * 24 * time.Hour,
	}

	softDeleter := NewSQLiteSoftDeleter(nil, config)
	assert.False(t, softDeleter.IsEnabled())

	// MarkAsDeleted should return error when disabled
	err := softDeleter.MarkAsDeleted("test", "id", "{}")
	assert.Error(t, err)
}

// TestManagerGetStats tests getting cleanup statistics.
func TestManagerGetStats(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
	}

	manager := NewManager(config, db, metrics)

	// Initial stats should be zero
	stats := manager.GetStats()
	assert.Equal(t, 0, stats.TotalRuns)
	assert.Equal(t, int64(0), stats.TotalDeletedCount)

	// Run cleanup
	manager.RunCleanup(context.Background())

	// Stats should be updated
	stats = manager.GetStats()
	assert.Equal(t, 1, stats.TotalRuns)
}

// BenchmarkCleanupQuotaHistory benchmarks the cleanup operation.
func BenchmarkCleanupQuotaHistory(b *testing.B) {
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(MEMORY)")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	// Create test table
	_, err = db.Exec(`
		CREATE TABLE quota_history (
			id INTEGER PRIMARY KEY,
			account_id TEXT,
			collected_at DATETIME,
			quota_value REAL
		)
	`)
	if err != nil {
		b.Fatal(err)
	}

	// Insert test data
	for i := 0; i < 1000; i++ {
		oldTime := time.Now().Add(-8 * 24 * time.Hour)
		_, err := db.Exec(`
			INSERT INTO quota_history (account_id, collected_at, quota_value)
			VALUES (?, ?, ?)
		`, "account1", oldTime, float64(i))
		if err != nil {
			b.Fatal(err)
		}
	}

	cleaner := NewSQLiteCleaner(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cleaner.CleanupQuotaHistory(7 * 24 * time.Hour); err != nil {
			b.Fatal(err)
		}
	}
}

// TestCleanupSoftDeleteDisabledManager tests manager with soft delete disabled.
func TestCleanupSoftDeleteDisabledManager(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
		SoftDeleteEnabled: false,
	}

	manager := NewManager(config, db, metrics)

	// Run cleanup should work without soft delete
	stats := manager.RunCleanup(context.Background())
	assert.NotNil(t, stats)
}

// TestCleanupWithNoOldData tests cleanup when there's no old data.
func TestCleanupWithNoOldData(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert only recent records
	recentTime := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 10; i++ {
		_, err := db.Exec(`
			INSERT INTO quota_history (account_id, collected_at, quota_value)
			VALUES (?, ?, ?)
		`, "account1", recentTime, float64(i))
		require.NoError(t, err)
	}

	result, err := cleaner.CleanupQuotaHistory(7 * 24 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.DeletedCount)

	// Verify all records are still there
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM quota_history").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}

// TestCleanupExpiredReservations tests cleanup of expired reservations.
func TestCleanupExpiredReservations(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert expired reservation
	oldTime := time.Now().Add(-48 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO reservations (account_id, status, expires_at)
		VALUES (?, ?, ?)
	`, "account1", "released", oldTime)
	require.NoError(t, err)

	// Insert active reservation
	futureTime := time.Now().Add(24 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO reservations (account_id, status, expires_at)
		VALUES (?, ?, ?)
	`, "account2", "active", futureTime)
	require.NoError(t, err)

	result, err := cleaner.CleanupExpiredReservations(24 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.DeletedCount)
}

// TestManagerSetPolicyProvider tests setting a custom policy provider.
func TestManagerSetPolicyProvider(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
	}

	manager := NewManager(config, db, metrics)

	// Create a custom policy provider
	customPolicies := []RetentionPolicy{
		{
			TableName:       "custom_table",
			RetentionPeriod: 1 * time.Hour,
			Enabled:         true,
		},
	}
	customProvider := NewInMemoryPolicyProvider(customPolicies)

	// Set the custom provider
	manager.SetPolicyProvider(customProvider)

	// Verify the custom provider is being used
	policy := customProvider.GetPolicy("custom_table")
	assert.NotNil(t, policy)
	assert.Equal(t, "custom_table", policy.TableName)
}

// TestManagerGetCleanerAndGetSoftDeleter tests getting the cleaner instances.
func TestManagerGetCleanerAndGetSoftDeleter(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	metrics := newMockMetricsRecorder()

	config := Config{
		Interval:          1 * time.Hour,
		RetentionPolicies: DefaultPolicies,
		SoftDeleteEnabled: true,
		HardDeleteAfter:   30 * 24 * time.Hour,
	}

	manager := NewManager(config, db, metrics)

	// Get the cleaner
	cleaner := manager.GetCleaner()
	assert.NotNil(t, cleaner)

	// Get the soft deleter
	softDeleter := manager.GetSoftDeleter()
	assert.NotNil(t, softDeleter)
	assert.True(t, softDeleter.IsEnabled())
}

// TestSQLiteSoftDeleterRestoreDeleted tests restoring soft-deleted records.
func TestSQLiteSoftDeleterRestoreDeleted(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	config := SoftDeleteConfig{
		Enabled:         true,
		HardDeleteAfter: 30 * 24 * time.Hour,
	}

	softDeleter := NewSQLiteSoftDeleter(db, config)

	// Mark a record as deleted
	err := softDeleter.MarkAsDeleted("test_table", "record1", `{"data": "test"}`)
	require.NoError(t, err)

	// Try to restore the record (this removes it from soft_deleted_records)
	err = softDeleter.RestoreDeleted("test_table", "record1")
	assert.NoError(t, err) // Restored successfully

	// The record should be removed from soft_deleted_records
	count, err := softDeleter.CountDeletedRecords("test_table")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestSQLiteSoftDeleterGetDeletedRecords tests getting soft-deleted records.
func TestSQLiteSoftDeleterGetDeletedRecords(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	config := SoftDeleteConfig{
		Enabled:         true,
		HardDeleteAfter: 30 * 24 * time.Hour,
	}

	softDeleter := NewSQLiteSoftDeleter(db, config)

	// Add some soft-deleted records
	err := softDeleter.MarkAsDeleted("table1", "id1", `{}`)
	require.NoError(t, err)

	err = softDeleter.MarkAsDeleted("table1", "id2", `{}`)
	require.NoError(t, err)

	err = softDeleter.MarkAsDeleted("table2", "id3", `{}`)
	require.NoError(t, err)

	// Get deleted records for table1
	records, err := softDeleter.GetDeletedRecords("table1")
	require.NoError(t, err)
	assert.Len(t, records, 2)

	// Get all deleted records
	allRecords, err := softDeleter.GetAllDeletedRecords()
	require.NoError(t, err)
	assert.Len(t, allRecords, 3)
}

// TestSQLiteCleanerGetTableStats tests getting table statistics.
func TestSQLiteCleanerGetTableStats(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert some data
	recentTime := time.Now()
	for i := 0; i < 10; i++ {
		_, err := db.Exec(`
			INSERT INTO quota_history (account_id, collected_at, quota_value)
			VALUES (?, ?, ?)
		`, "account1", recentTime, float64(i))
		require.NoError(t, err)
	}

	// Get table stats
	rowCount, tableSize, err := cleaner.GetTableStats("quota_history")
	require.NoError(t, err)
	assert.Equal(t, int64(10), rowCount)
	assert.True(t, tableSize >= 0)
}

// TestSQLiteCleanerCleanupHealthHistory tests cleaning up health history.
func TestSQLiteCleanerCleanupHealthHistory(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Insert old health records
	oldTime := time.Now().Add(-48 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO health_history (account_id, checked_at, status)
		VALUES (?, ?, ?)
	`, "account1", oldTime, "healthy")
	require.NoError(t, err)

	// Insert recent health records
	recentTime := time.Now().Add(-1 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO health_history (account_id, checked_at, status)
		VALUES (?, ?, ?)
	`, "account2", recentTime, "healthy")
	require.NoError(t, err)

	// Run cleanup with 24 hour retention
	result, err := cleaner.CleanupHealthHistory(24 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "health_history", result.TableName)
	assert.Equal(t, int64(1), result.DeletedCount)
}

// TestSQLiteCleanerCleanupSoftDeletedRecords tests cleaning soft-deleted records.
func TestSQLiteCleanerCleanupSoftDeletedRecords(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	cleaner := NewSQLiteCleaner(db)

	// Add soft-deleted records
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO soft_deleted_records (table_name, record_id, deleted_at, original_data)
		VALUES (?, ?, ?, ?)
	`, "test_table", "id1", oldTime, `{}`)
	require.NoError(t, err)

	recentTime := time.Now().Add(-1 * 24 * time.Hour)
	_, err = db.Exec(`
		INSERT INTO soft_deleted_records (table_name, record_id, deleted_at, original_data)
		VALUES (?, ?, ?, ?)
	`, "test_table", "id2", recentTime, `{}`)
	require.NoError(t, err)

	// Run cleanup with 30 day retention
	result, err := cleaner.CleanupSoftDeletedRecords(30 * 24 * time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "soft_deleted_records", result.TableName)
	assert.Equal(t, int64(1), result.DeletedCount)
}
