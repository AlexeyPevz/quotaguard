package errors

import "fmt"

// Config errors

type ErrConfigNotFound struct {
	Path string
}

func (e *ErrConfigNotFound) Error() string {
	return fmt.Sprintf("config file not found: %s", e.Path)
}

type ErrConfigParse struct {
	Err error
}

func (e *ErrConfigParse) Error() string {
	return fmt.Sprintf("failed to parse YAML: %v", e.Err)
}

func (e *ErrConfigParse) Unwrap() error {
	return e.Err
}

type ErrConfigValidation struct {
	Err error
}

func (e *ErrConfigValidation) Error() string {
	return fmt.Sprintf("config validation failed: %v", e.Err)
}

func (e *ErrConfigValidation) Unwrap() error {
	return e.Err
}

// Database errors

type ErrDatabaseOpen struct {
	Path string
	Err  error
}

func (e *ErrDatabaseOpen) Error() string {
	return fmt.Sprintf("failed to open database %s: %v", e.Path, e.Err)
}

func (e *ErrDatabaseOpen) Unwrap() error {
	return e.Err
}

type ErrDatabaseMigration struct {
	Version int
	Err     error
}

func (e *ErrDatabaseMigration) Error() string {
	return fmt.Sprintf("database migration %d failed: %v", e.Version, e.Err)
}

func (e *ErrDatabaseMigration) Unwrap() error {
	return e.Err
}

type ErrDatabaseQuery struct {
	Operation string
	Err       error
}

func (e *ErrDatabaseQuery) Error() string {
	return fmt.Sprintf("database query failed for operation %s: %v", e.Operation, e.Err)
}

func (e *ErrDatabaseQuery) Unwrap() error {
	return e.Err
}

// Validation errors

type ErrQuotaValidation struct {
	Field     string
	AccountID string
	Err       error
}

func (e *ErrQuotaValidation) Error() string {
	return fmt.Sprintf("quota validation error for %s: %v", e.AccountID, e.Err)
}

func (e *ErrQuotaValidation) Unwrap() error {
	return e.Err
}

// Server errors

type ErrServerStart struct {
	Addr string
	Err  error
}

func (e *ErrServerStart) Error() string {
	return fmt.Sprintf("failed to start server on %s: %v", e.Addr, e.Err)
}

func (e *ErrServerStart) Unwrap() error {
	return e.Err
}

type ErrServerShutdown struct {
	Err error
}

func (e *ErrServerShutdown) Error() string {
	return fmt.Sprintf("server shutdown failed: %v", e.Err)
}

func (e *ErrServerShutdown) Unwrap() error {
	return e.Err
}

// Filesystem errors

type ErrDirectoryCreate struct {
	Path string
	Err  error
}

func (e *ErrDirectoryCreate) Error() string {
	return fmt.Sprintf("failed to create directory %s: %v", e.Path, e.Err)
}

func (e *ErrDirectoryCreate) Unwrap() error {
	return e.Err
}

type ErrFileRead struct {
	Path string
	Err  error
}

func (e *ErrFileRead) Error() string {
	return fmt.Sprintf("failed to read file %s: %v", e.Path, e.Err)
}

func (e *ErrFileRead) Unwrap() error {
	return e.Err
}

// Router errors

type ErrNoSuitableAccounts struct {
	Reason string
}

func (e *ErrNoSuitableAccounts) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("no suitable accounts found: %s", e.Reason)
	}
	return "no suitable accounts found"
}
