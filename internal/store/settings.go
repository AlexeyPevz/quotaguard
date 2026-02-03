package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Setting represents a key-value setting stored in SQLite
type Setting struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// SettingsStore provides methods for managing dynamic settings
type SettingsStore interface {
	Get(key string) (string, bool)
	Set(key, value string) error
	Delete(key string) error
	GetInt(key string, defaultVal int) int
	SetInt(key string, value int) error
	GetFloat(key string, defaultVal float64) float64
	SetFloat(key string, value float64) error
	GetBool(key string, defaultVal bool) bool
	SetBool(key string, value bool) error
}

// SQLiteSettingsStore implements SettingsStore using SQLite
type SQLiteSettingsStore struct {
	db *sql.DB
}

// NewSQLiteSettingsStore creates a new settings store
func NewSQLiteSettingsStore(db *sql.DB) (*SQLiteSettingsStore, error) {
	store := &SQLiteSettingsStore{db: db}

	// Create settings table if not exists
	if err := store.createTable(); err != nil {
		return nil, err
	}

	return store, nil
}

// createTable creates the settings table
func (s *SQLiteSettingsStore) createTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`
	_, err := s.db.Exec(query)
	return err
}

// Get retrieves a setting value
func (s *SQLiteSettingsStore) Get(key string) (string, bool) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return value, true
}

// Set sets a setting value
func (s *SQLiteSettingsStore) Set(key, value string) error {
	query := `
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`
	now := time.Now()
	_, err := s.db.Exec(query, key, value, now, value, now)
	return err
}

// Delete removes a setting
func (s *SQLiteSettingsStore) Delete(key string) error {
	_, err := s.db.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

// GetInt retrieves an integer setting
func (s *SQLiteSettingsStore) GetInt(key string, defaultVal int) int {
	value, ok := s.Get(key)
	if !ok {
		return defaultVal
	}
	var result int
	_, err := fmt.Sscanf(value, "%d", &result)
	if err != nil {
		return defaultVal
	}
	return result
}

// SetInt sets an integer setting
func (s *SQLiteSettingsStore) SetInt(key string, value int) error {
	return s.Set(key, fmt.Sprintf("%d", value))
}

// GetFloat retrieves a float setting
func (s *SQLiteSettingsStore) GetFloat(key string, defaultVal float64) float64 {
	value, ok := s.Get(key)
	if !ok {
		return defaultVal
	}
	var result float64
	_, err := fmt.Sscanf(value, "%f", &result)
	if err != nil {
		return defaultVal
	}
	return result
}

// SetFloat sets a float setting
func (s *SQLiteSettingsStore) SetFloat(key string, value float64) error {
	return s.Set(key, fmt.Sprintf("%f", value))
}

// GetBool retrieves a bool setting
func (s *SQLiteSettingsStore) GetBool(key string, defaultVal bool) bool {
	value, ok := s.Get(key)
	if !ok {
		return defaultVal
	}
	return value == "true" || value == "1" || value == "yes"
}

// SetBool sets a bool setting
func (s *SQLiteSettingsStore) SetBool(key string, value bool) error {
	if value {
		return s.Set(key, "true")
	}
	return s.Set(key, "false")
}

// Constants for setting keys
const (
	SettingTelegramBotToken   = "telegram_bot_token"
	SettingTelegramChatID     = "telegram_chat_id"
	SettingCodexSessionToken  = "codex_session_token"
	SettingThresholdsWarning  = "thresholds_warning"
	SettingThresholdsSwitch   = "thresholds_switch"
	SettingThresholdsCritical = "thresholds_critical"
	SettingRoutingPolicy      = "routing_policy"
	SettingFallbackChains     = "fallback_chains"
	SettingAlertsEnabled      = "alerts_enabled"
	SettingAlertsThreshold    = "alerts_threshold"
)
