package cleanup

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// MetricsRecorder defines the interface for recording cleanup metrics.
type MetricsRecorder interface {
	RecordCleanupOperation(tableName string, deletedCount int64, duration time.Duration)
	RecordVacuumOperation(duration time.Duration)
	RecordAnalyzeOperation(duration time.Duration)
}

// Config contains the cleanup manager configuration.
type Config struct {
	Interval          time.Duration     `json:"interval"`
	RetentionPolicies []RetentionPolicy `json:"retention_policies"`
	SoftDeleteEnabled bool              `json:"soft_delete_enabled"`
	HardDeleteAfter   time.Duration     `json:"hard_delete_after"`
	VacuumEnabled     bool              `json:"vacuum_enabled"`
	VacuumInterval    time.Duration     `json:"vacuum_interval"`
	AnalyzeEnabled    bool              `json:"analyze_enabled"`
	AnalyzeInterval   time.Duration     `json:"analyze_interval"`
	ShutdownTimeout   time.Duration     `json:"shutdown_timeout"`
	BatchSize         int               `json:"batch_size"`
}

// Stats contains cleanup statistics.
type Stats struct {
	TotalRuns         int              `json:"total_runs"`
	TotalDeletedCount int64            `json:"total_deleted_count"`
	LastRunAt         time.Time        `json:"last_run_at"`
	LastRunDuration   time.Duration    `json:"last_run_duration"`
	LastRunResults    []*CleanupResult `json:"last_run_results"`
	VacuumCount       int              `json:"vacuum_count"`
	VacuumLastAt      time.Time        `json:"vacuum_last_at"`
	AnalyzeCount      int              `json:"analyze_count"`
	AnalyzeLastAt     time.Time        `json:"analyze_last_at"`
	Mu                sync.RWMutex
}

// Manager handles periodic cleanup of old data.
type Manager struct {
	db          *sql.DB
	config      Config
	provider    PolicyProvider
	cleaner     *SQLiteCleaner
	softDeleter *SQLiteSoftDeleter
	metrics     MetricsRecorder

	ticker        *time.Ticker
	vacuumTicker  *time.Ticker
	analyzeTicker *time.Ticker
	done          chan struct{}
	running       bool
	mu            sync.Mutex
	stats         Stats
}

// NewManager creates a new cleanup manager.
func NewManager(config Config, db *sql.DB, metrics MetricsRecorder) *Manager {
	provider := NewInMemoryPolicyProvider(config.RetentionPolicies)

	softDeleteConfig := SoftDeleteConfig{
		Enabled:         config.SoftDeleteEnabled,
		HardDeleteAfter: config.HardDeleteAfter,
	}

	return &Manager{
		db:          db,
		config:      config,
		provider:    provider,
		cleaner:     NewSQLiteCleaner(db),
		softDeleter: NewSQLiteSoftDeleter(db, softDeleteConfig),
		metrics:     metrics,
		done:        make(chan struct{}),
	}
}

// SetPolicyProvider sets a custom policy provider.
func (m *Manager) SetPolicyProvider(provider PolicyProvider) {
	m.provider = provider
}

// Start starts the cleanup manager.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("cleanup manager is already running")
	}

	m.running = true
	m.stats.LastRunAt = time.Now()

	// Start cleanup ticker
	if m.config.Interval > 0 {
		m.ticker = time.NewTicker(m.config.Interval)
		go m.runCleanupLoop(ctx)
	}

	// Start vacuum ticker
	if m.config.VacuumEnabled && m.config.VacuumInterval > 0 {
		m.vacuumTicker = time.NewTicker(m.config.VacuumInterval)
		go m.runVacuumLoop(ctx)
	}

	// Start analyze ticker
	if m.config.AnalyzeEnabled && m.config.AnalyzeInterval > 0 {
		m.analyzeTicker = time.NewTicker(m.config.AnalyzeInterval)
		go m.runAnalyzeLoop(ctx)
	}

	return nil
}

// Stop stops the cleanup manager gracefully.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	m.running = false

	// Stop all tickers
	if m.ticker != nil {
		m.ticker.Stop()
	}
	if m.vacuumTicker != nil {
		m.vacuumTicker.Stop()
	}
	if m.analyzeTicker != nil {
		m.analyzeTicker.Stop()
	}

	// Close done channel to signal goroutines to stop
	close(m.done)

	return nil
}

// runCleanupLoop runs the periodic cleanup loop.
func (m *Manager) runCleanupLoop(ctx context.Context) {
	for {
		select {
		case <-m.done:
			return
		case <-ctx.Done():
			return
		case <-m.ticker.C:
			m.RunCleanup(ctx)
		}
	}
}

// runVacuumLoop runs the periodic vacuum loop.
func (m *Manager) runVacuumLoop(ctx context.Context) {
	for {
		select {
		case <-m.done:
			return
		case <-ctx.Done():
			return
		case <-m.vacuumTicker.C:
			if err := m.RunVacuum(ctx); err != nil {
				// Best-effort maintenance; ignore errors in background loop.
				_ = err
			}
		}
	}
}

// runAnalyzeLoop runs the periodic analyze loop.
func (m *Manager) runAnalyzeLoop(ctx context.Context) {
	for {
		select {
		case <-m.done:
			return
		case <-ctx.Done():
			return
		case <-m.analyzeTicker.C:
			if err := m.RunAnalyze(ctx); err != nil {
				// Best-effort maintenance; ignore errors in background loop.
				_ = err
			}
		}
	}
}

// RunCleanup performs a cleanup operation immediately.
func (m *Manager) RunCleanup(ctx context.Context) *Stats {
	start := time.Now()

	results, err := m.cleaner.RunAllCleanup(m.provider)

	// Handle soft delete hard delete if enabled
	if m.softDeleter.IsEnabled() {
		hardDeleteResult, err := m.softDeleter.HardDeleteOld()
		if err == nil && hardDeleteResult != nil {
			results = append(results, hardDeleteResult)
		}
	}

	duration := time.Since(start)

	// Update stats
	m.stats.Mu.Lock()
	m.stats.TotalRuns++
	m.stats.LastRunAt = time.Now()
	m.stats.LastRunDuration = duration
	m.stats.LastRunResults = results

	for _, result := range results {
		if result.Error == nil {
			m.stats.TotalDeletedCount += result.DeletedCount
		}
	}
	m.stats.Mu.Unlock()

	// Record metrics
	if m.metrics != nil {
		for _, result := range results {
			if result.Error == nil {
				m.metrics.RecordCleanupOperation(result.TableName, result.DeletedCount, result.Duration)
			}
		}
	}

	if err != nil {
		_ = ctx // Context is used for graceful shutdown
	}

	return m.GetStats()
}

// RunVacuum performs a vacuum operation immediately.
func (m *Manager) RunVacuum(ctx context.Context) error {
	start := time.Now()

	err := m.cleaner.VacuumDatabase()

	duration := time.Since(start)

	// Update stats
	m.stats.Mu.Lock()
	m.stats.VacuumCount++
	m.stats.VacuumLastAt = time.Now()
	m.stats.Mu.Unlock()

	// Record metrics
	if m.metrics != nil && err == nil {
		m.metrics.RecordVacuumOperation(duration)
	}

	return err
}

// RunAnalyze performs an analyze operation immediately.
func (m *Manager) RunAnalyze(ctx context.Context) error {
	start := time.Now()

	err := m.cleaner.AnalyzeDatabase()

	duration := time.Since(start)

	// Update stats
	m.stats.Mu.Lock()
	m.stats.AnalyzeCount++
	m.stats.AnalyzeLastAt = time.Now()
	m.stats.Mu.Unlock()

	// Record metrics
	if m.metrics != nil && err == nil {
		m.metrics.RecordAnalyzeOperation(duration)
	}

	return err
}

// GetStats returns the current cleanup statistics.
func (m *Manager) GetStats() *Stats {
	m.stats.Mu.RLock()
	defer m.stats.Mu.RUnlock()

	return &Stats{
		TotalRuns:         m.stats.TotalRuns,
		TotalDeletedCount: m.stats.TotalDeletedCount,
		LastRunAt:         m.stats.LastRunAt,
		LastRunDuration:   m.stats.LastRunDuration,
		LastRunResults:    m.stats.LastRunResults,
		VacuumCount:       m.stats.VacuumCount,
		VacuumLastAt:      m.stats.VacuumLastAt,
		AnalyzeCount:      m.stats.AnalyzeCount,
		AnalyzeLastAt:     m.stats.AnalyzeLastAt,
	}
}

// GetCleaner returns the SQLite cleaner instance.
func (m *Manager) GetCleaner() *SQLiteCleaner {
	return m.cleaner
}

// GetSoftDeleter returns the soft deleter instance.
func (m *Manager) GetSoftDeleter() *SQLiteSoftDeleter {
	return m.softDeleter
}

// IsRunning returns whether the cleanup manager is running.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}
