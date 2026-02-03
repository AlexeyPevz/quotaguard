package cleanup

import (
	"database/sql"
	"fmt"
	"time"
)

// SQLiteCleaner provides SQLite-specific cleanup operations.
type SQLiteCleaner struct {
	db *sql.DB
}

// NewSQLiteCleaner creates a new SQLite cleaner.
func NewSQLiteCleaner(db *sql.DB) *SQLiteCleaner {
	return &SQLiteCleaner{db: db}
}

// CleanupResult holds the result of a cleanup operation.
type CleanupResult struct {
	TableName    string
	DeletedCount int64
	Duration     time.Duration
	Error        error
}

// CleanupQuotaHistory removes old quota history records.
func (c *SQLiteCleaner) CleanupQuotaHistory(retentionPeriod time.Duration) (*CleanupResult, error) {
	start := time.Now()

	cutoff := time.Now().Add(-retentionPeriod)

	result, err := c.db.Exec(`
		DELETE FROM quota_history
		WHERE collected_at < ?
	`, cutoff)

	if err != nil {
		return &CleanupResult{
			TableName: "quota_history",
			Error:     fmt.Errorf("failed to cleanup quota_history: %w", err),
			Duration:  time.Since(start),
		}, err
	}

	rowsAffected, _ := result.RowsAffected()

	return &CleanupResult{
		TableName:    "quota_history",
		DeletedCount: rowsAffected,
		Duration:     time.Since(start),
	}, nil
}

// CleanupRoutingEvents removes old routing event records.
func (c *SQLiteCleaner) CleanupRoutingEvents(retentionPeriod time.Duration) (*CleanupResult, error) {
	start := time.Now()

	cutoff := time.Now().Add(-retentionPeriod)

	result, err := c.db.Exec(`
		DELETE FROM routing_events
		WHERE created_at < ?
	`, cutoff)

	if err != nil {
		return &CleanupResult{
			TableName: "routing_events",
			Error:     fmt.Errorf("failed to cleanup routing_events: %w", err),
			Duration:  time.Since(start),
		}, err
	}

	rowsAffected, _ := result.RowsAffected()

	return &CleanupResult{
		TableName:    "routing_events",
		DeletedCount: rowsAffected,
		Duration:     time.Since(start),
	}, nil
}

// CleanupHealthHistory removes old health history records.
func (c *SQLiteCleaner) CleanupHealthHistory(retentionPeriod time.Duration) (*CleanupResult, error) {
	start := time.Now()

	cutoff := time.Now().Add(-retentionPeriod)

	result, err := c.db.Exec(`
		DELETE FROM health_history
		WHERE checked_at < ?
	`, cutoff)

	if err != nil {
		return &CleanupResult{
			TableName: "health_history",
			Error:     fmt.Errorf("failed to cleanup health_history: %w", err),
			Duration:  time.Since(start),
		}, err
	}

	rowsAffected, _ := result.RowsAffected()

	return &CleanupResult{
		TableName:    "health_history",
		DeletedCount: rowsAffected,
		Duration:     time.Since(start),
	}, nil
}

// CleanupAlerts removes old alert records.
func (c *SQLiteCleaner) CleanupAlerts(retentionPeriod time.Duration) (*CleanupResult, error) {
	start := time.Now()

	cutoff := time.Now().Add(-retentionPeriod)

	result, err := c.db.Exec(`
		DELETE FROM alerts
		WHERE created_at < ?
	`, cutoff)

	if err != nil {
		return &CleanupResult{
			TableName: "alerts",
			Error:     fmt.Errorf("failed to cleanup alerts: %w", err),
			Duration:  time.Since(start),
		}, err
	}

	rowsAffected, _ := result.RowsAffected()

	return &CleanupResult{
		TableName:    "alerts",
		DeletedCount: rowsAffected,
		Duration:     time.Since(start),
	}, nil
}

// CleanupExpiredReservations removes expired reservation records.
func (c *SQLiteCleaner) CleanupExpiredReservations(retentionPeriod time.Duration) (*CleanupResult, error) {
	start := time.Now()

	cutoff := time.Now().Add(-retentionPeriod)

	// Only delete reservations that are in a terminal state (released, cancelled, or expired)
	result, err := c.db.Exec(`
		DELETE FROM reservations
		WHERE status IN ('released', 'cancelled')
		AND expires_at < ?
	`, cutoff)

	if err != nil {
		return &CleanupResult{
			TableName: "reservations",
			Error:     fmt.Errorf("failed to cleanup reservations: %w", err),
			Duration:  time.Since(start),
		}, err
	}

	rowsAffected, _ := result.RowsAffected()

	return &CleanupResult{
		TableName:    "reservations",
		DeletedCount: rowsAffected,
		Duration:     time.Since(start),
	}, nil
}

// CleanupSoftDeletedRecords permanently removes soft-deleted records that have passed their grace period.
func (c *SQLiteCleaner) CleanupSoftDeletedRecords(retentionPeriod time.Duration) (*CleanupResult, error) {
	start := time.Now()

	cutoff := time.Now().Add(-retentionPeriod)

	result, err := c.db.Exec(`
		DELETE FROM soft_deleted_records
		WHERE deleted_at < ?
	`, cutoff)

	if err != nil {
		return &CleanupResult{
			TableName: "soft_deleted_records",
			Error:     fmt.Errorf("failed to cleanup soft_deleted_records: %w", err),
			Duration:  time.Since(start),
		}, err
	}

	rowsAffected, _ := result.RowsAffected()

	return &CleanupResult{
		TableName:    "soft_deleted_records",
		DeletedCount: rowsAffected,
		Duration:     time.Since(start),
	}, nil
}

// VacuumDatabase runs VACUUM to reclaim disk space and optimize the database.
func (c *SQLiteCleaner) VacuumDatabase() error {
	start := time.Now()

	_, err := c.db.Exec("VACUUM")
	if err != nil {
		return fmt.Errorf("failed to vacuum database: %w", err)
	}

	_ = start // Used for logging in production

	return nil
}

// AnalyzeDatabase runs ANALYZE to update database statistics for query optimization.
func (c *SQLiteCleaner) AnalyzeDatabase() error {
	start := time.Now()

	_, err := c.db.Exec("ANALYZE")
	if err != nil {
		return fmt.Errorf("failed to analyze database: %w", err)
	}

	_ = start // Used for logging in production

	return nil
}

// GetTableStats returns statistics about a specific table.
func (c *SQLiteCleaner) GetTableStats(tableName string) (int64, int64, error) {
	// Get row count
	var rowCount int64
	err := c.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&rowCount)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get row count for %s: %w", tableName, err)
	}

	// Get table size in bytes
	var tableSize int64
	err = c.db.QueryRow(`
		SELECT COALESCE(SUM(pgsize), 0)
		FROM dbstat
		WHERE name = ?
	`, tableName).Scan(&tableSize)
	if err != nil {
		// dbstat might not be enabled, return just row count
		return rowCount, 0, nil
	}

	return rowCount, tableSize, nil
}

// RunAllCleanup performs cleanup on all tables based on default policies.
func (c *SQLiteCleaner) RunAllCleanup(provider PolicyProvider) ([]*CleanupResult, error) {
	results := make([]*CleanupResult, 0)
	policies := provider.GetAllPolicies()

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		var result *CleanupResult
		var err error

		switch policy.TableName {
		case "quota_history":
			result, err = c.CleanupQuotaHistory(policy.RetentionPeriod)
		case "routing_events":
			result, err = c.CleanupRoutingEvents(policy.RetentionPeriod)
		case "health_history":
			result, err = c.CleanupHealthHistory(policy.RetentionPeriod)
		case "alerts":
			result, err = c.CleanupAlerts(policy.RetentionPeriod)
		case "reservations":
			result, err = c.CleanupExpiredReservations(policy.RetentionPeriod)
		case "soft_deleted_records":
			result, err = c.CleanupSoftDeletedRecords(policy.RetentionPeriod)
		}

		if err != nil {
			results = append(results, result)
		} else if result != nil {
			results = append(results, result)
		}
	}

	return results, nil
}
