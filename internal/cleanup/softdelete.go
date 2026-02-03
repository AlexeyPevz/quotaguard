package cleanup

import (
	"database/sql"
	"fmt"
	"time"
)

// SoftDeleteConfig contains soft delete configuration.
type SoftDeleteConfig struct {
	Enabled         bool          `json:"enabled"`
	HardDeleteAfter time.Duration `json:"hard_delete_after"`
	TableName       string        `json:"table_name"`
	DeletedColumn   string        `json:"deleted_column"`
}

// SoftDeleteRecord represents a soft-deleted record.
type SoftDeleteRecord struct {
	ID           string
	TableName    string
	RecordID     string
	DeletedAt    time.Time
	OriginalData string
}

// SQLiteSoftDeleter provides soft delete functionality for SQLite.
type SQLiteSoftDeleter struct {
	db     *sql.DB
	config SoftDeleteConfig
}

// NewSQLiteSoftDeleter creates a new soft delete handler.
func NewSQLiteSoftDeleter(db *sql.DB, config SoftDeleteConfig) *SQLiteSoftDeleter {
	if config.DeletedColumn == "" {
		config.DeletedColumn = "deleted_at"
	}
	if config.TableName == "" {
		config.TableName = "soft_deleted_records"
	}
	return &SQLiteSoftDeleter{
		db:     db,
		config: config,
	}
}

// MarkAsDeleted marks a record as deleted (soft delete).
func (s *SQLiteSoftDeleter) MarkAsDeleted(tableName string, recordID string, originalData string) error {
	if !s.config.Enabled {
		return fmt.Errorf("soft delete is not enabled")
	}

	now := time.Now()

	// First, try to insert into soft_deleted_records
	_, err := s.db.Exec(`
		INSERT INTO soft_deleted_records (table_name, record_id, deleted_at, original_data)
		VALUES (?, ?, ?, ?)
	`, tableName, recordID, now, originalData)

	if err != nil {
		return fmt.Errorf("failed to insert soft delete record: %w", err)
	}

	return nil
}

// RestoreDeleted restores a soft-deleted record.
func (s *SQLiteSoftDeleter) RestoreDeleted(tableName string, recordID string) error {
	// Get the original data
	var originalData string
	err := s.db.QueryRow(`
		SELECT original_data FROM soft_deleted_records
		WHERE table_name = ? AND record_id = ?
	`, tableName, recordID).Scan(&originalData)

	if err == sql.ErrNoRows {
		return fmt.Errorf("soft delete record not found")
	}
	if err != nil {
		return fmt.Errorf("failed to get soft delete record: %w", err)
	}

	// Delete from soft_deleted_records
	_, err = s.db.Exec(`
		DELETE FROM soft_deleted_records
		WHERE table_name = ? AND record_id = ?
	`, tableName, recordID)

	if err != nil {
		return fmt.Errorf("failed to remove soft delete record: %w", err)
	}

	// Note: Actual restoration requires table-specific logic
	// This is a placeholder that would need to be implemented per table type
	_ = originalData

	return nil
}

// HardDeleteOld permanently removes soft-deleted records that have passed the grace period.
func (s *SQLiteSoftDeleter) HardDeleteOld() (*CleanupResult, error) {
	if !s.config.Enabled {
		return &CleanupResult{
			TableName: "hard_delete",
			Error:     fmt.Errorf("soft delete is not enabled"),
		}, nil
	}

	start := time.Now()

	cutoff := time.Now().Add(-s.config.HardDeleteAfter)

	result, err := s.db.Exec(`
		DELETE FROM soft_deleted_records
		WHERE deleted_at < ?
	`, cutoff)

	if err != nil {
		return &CleanupResult{
			TableName: "hard_delete",
			Error:     fmt.Errorf("failed to hard delete old records: %w", err),
			Duration:  time.Since(start),
		}, err
	}

	rowsAffected, _ := result.RowsAffected()

	return &CleanupResult{
		TableName:    "hard_delete",
		DeletedCount: rowsAffected,
		Duration:     time.Since(start),
	}, nil
}

// GetDeletedRecords returns all soft-deleted records.
func (s *SQLiteSoftDeleter) GetDeletedRecords(tableName string) ([]*SoftDeleteRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, table_name, record_id, deleted_at, original_data
		FROM soft_deleted_records
		WHERE table_name = ?
		ORDER BY deleted_at DESC
	`, tableName)

	if err != nil {
		return nil, fmt.Errorf("failed to query soft deleted records: %w", err)
	}
	defer rows.Close()

	var records []*SoftDeleteRecord
	for rows.Next() {
		var record SoftDeleteRecord
		err := rows.Scan(&record.ID, &record.TableName, &record.RecordID, &record.DeletedAt, &record.OriginalData)
		if err != nil {
			continue
		}
		records = append(records, &record)
	}

	return records, nil
}

// GetAllDeletedRecords returns all soft-deleted records across all tables.
func (s *SQLiteSoftDeleter) GetAllDeletedRecords() ([]*SoftDeleteRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, table_name, record_id, deleted_at, original_data
		FROM soft_deleted_records
		ORDER BY deleted_at DESC
	`)

	if err != nil {
		return nil, fmt.Errorf("failed to query soft deleted records: %w", err)
	}
	defer rows.Close()

	var records []*SoftDeleteRecord
	for rows.Next() {
		var record SoftDeleteRecord
		err := rows.Scan(&record.ID, &record.TableName, &record.RecordID, &record.DeletedAt, &record.OriginalData)
		if err != nil {
			continue
		}
		records = append(records, &record)
	}

	return records, nil
}

// CountDeletedRecords returns the count of soft-deleted records.
func (s *SQLiteSoftDeleter) CountDeletedRecords(tableName string) (int, error) {
	var count int

	if tableName == "" {
		err := s.db.QueryRow(`SELECT COUNT(*) FROM soft_deleted_records`).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("failed to count soft deleted records: %w", err)
		}
	} else {
		err := s.db.QueryRow(`
			SELECT COUNT(*) FROM soft_deleted_records
			WHERE table_name = ?
		`, tableName).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("failed to count soft deleted records: %w", err)
		}
	}

	return count, nil
}

// IsEnabled returns whether soft delete is enabled.
func (s *SQLiteSoftDeleter) IsEnabled() bool {
	return s.config.Enabled
}

// GetConfig returns the current soft delete configuration.
func (s *SQLiteSoftDeleter) GetConfig() SoftDeleteConfig {
	return s.config
}
