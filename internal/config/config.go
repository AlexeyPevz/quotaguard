package config

import (
	"fmt"
	"time"
)

// Config represents the complete application configuration.
type Config struct {
	Version    string           `yaml:"version"`
	Server     ServerConfig     `yaml:"server"`
	API        APIConfig        `yaml:"api"`
	Collector  CollectorConfig  `yaml:"collector"`
	Router     RouterConfig     `yaml:"router"`
	Health     HealthConfig     `yaml:"health"`
	Telegram   TelegramConfig   `yaml:"telegram"`
	Middleware MiddlewareConfig `yaml:"middleware"`
	Alerts     AlertsConfig     `yaml:"alerts"`
	Cleanup    CleanupConfig    `yaml:"cleanup"`
	Accounts   []AccountConfig  `yaml:"accounts,omitempty"`
}

// ServerConfig contains server-related configuration.
type ServerConfig struct {
	Host            string        `yaml:"host"`
	HTTPPort        int           `yaml:"http_port"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	LogLevel        string        `yaml:"log_level"`
	LogFormat       string        `yaml:"log_format"`
	TLS             TLSConfig     `yaml:"tls"`
}

// TLSConfig contains TLS configuration.
type TLSConfig struct {
	Enabled    bool   `yaml:"enabled"`
	CertFile   string `yaml:"cert_file"`
	KeyFile    string `yaml:"key_file"`
	MinVersion string `yaml:"min_version"` // "1.2" or "1.3"
}

// APIConfig contains API-related configuration.
type APIConfig struct {
	Enabled   bool            `yaml:"enabled"`
	BasePath  string          `yaml:"base_path"`
	Auth      AuthConfig      `yaml:"auth"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	CORS      CORSConfig      `yaml:"cors"`
}

// AuthConfig contains authentication configuration.
type AuthConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Type       string   `yaml:"type"`
	Secret     string   `yaml:"secret"`
	APIKeys    []string `yaml:"api_keys"`
	HeaderName string   `yaml:"header_name"`
}

// RateLimitConfig contains rate limiting configuration.
type RateLimitConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
	Burst             int `yaml:"burst"`
}

// CORSConfig contains CORS configuration.
type CORSConfig struct {
	Enabled bool     `yaml:"enabled"`
	Origins []string `yaml:"origins"`
	Methods []string `yaml:"methods"`
}

// CollectorConfig contains collector configuration.
type CollectorConfig struct {
	Mode    string                 `yaml:"mode"`
	Passive PassiveCollectorConfig `yaml:"passive"`
	Active  ActiveCollectorConfig  `yaml:"active"`
}

// PassiveCollectorConfig contains passive collector configuration.
type PassiveCollectorConfig struct {
	Enabled       bool          `yaml:"enabled"`
	BufferSize    int           `yaml:"buffer_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`
}

// ActiveCollectorConfig contains active collector configuration.
type ActiveCollectorConfig struct {
	Enabled         bool                 `yaml:"enabled"`
	DefaultInterval time.Duration        `yaml:"default_interval"`
	Adaptive        bool                 `yaml:"adaptive"`
	Timeout         time.Duration        `yaml:"timeout"`
	RetryAttempts   int                  `yaml:"retry_attempts"`
	RetryBackoff    string               `yaml:"retry_backoff"`
	CircuitBreaker  CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// CircuitBreakerConfig contains circuit breaker configuration.
type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failure_threshold"`
	Timeout          time.Duration `yaml:"timeout"`
	HalfOpenLimit    int           `yaml:"half_open_limit"` // Number of successes in half-open state to close
}

// RouterConfig contains router configuration.
type RouterConfig struct {
	Thresholds     ThresholdsConfig     `yaml:"thresholds"`
	AntiFlapping   AntiFlappingConfig   `yaml:"anti_flapping"`
	Reservation    ReservationConfig    `yaml:"reservation"`
	Weights        WeightsConfig        `yaml:"weights"`
	Policies       []PolicyConfig       `yaml:"policies"`
	FallbackChains map[string][]string  `yaml:"fallback_chains"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
}

// ThresholdsConfig contains threshold configuration.
type ThresholdsConfig struct {
	Warning  float64 `yaml:"warning"`
	Switch   float64 `yaml:"switch"`
	Critical float64 `yaml:"critical"`
	MinSafe  float64 `yaml:"min_safe"`
}

// AntiFlappingConfig contains anti-flapping configuration.
type AntiFlappingConfig struct {
	MinDwellTime        time.Duration `yaml:"min_dwell_time"`
	CooldownAfterSwitch time.Duration `yaml:"cooldown_after_switch"`
	HysteresisMargin    float64       `yaml:"hysteresis_margin"`
}

// ReservationConfig contains reservation configuration.
type ReservationConfig struct {
	Enabled                     bool          `yaml:"enabled"`
	Timeout                     time.Duration `yaml:"timeout"`
	CleanupInterval             time.Duration `yaml:"cleanup_interval"`
	DefaultEstimatedCostPercent float64       `yaml:"default_estimated_cost_percent"`
}

// WeightsConfig contains routing weights configuration.
type WeightsConfig struct {
	Safety      float64 `yaml:"safety"`
	Refill      float64 `yaml:"refill"`
	Tier        float64 `yaml:"tier"`
	Reliability float64 `yaml:"reliability"`
	Cost        float64 `yaml:"cost"`
}

// PolicyConfig contains a routing policy configuration.
type PolicyConfig struct {
	Name    string        `yaml:"name"`
	Weights WeightsConfig `yaml:"weights"`
}

// HealthConfig contains health check configuration.
type HealthConfig struct {
	Enabled           bool                  `yaml:"enabled"`
	Interval          time.Duration         `yaml:"interval"`
	Timeout           time.Duration         `yaml:"timeout"`
	BaselineAnomaly   BaselineAnomalyConfig `yaml:"baseline_anomaly"`
	QualityChecks     QualityChecksConfig   `yaml:"quality_checks"`
	ProviderEndpoints map[string]string     `yaml:"provider_endpoints"` // provider -> endpoint URL
}

// BaselineAnomalyConfig contains baseline anomaly detection configuration.
type BaselineAnomalyConfig struct {
	Enabled                bool    `yaml:"enabled"`
	LatencySpikeMultiplier float64 `yaml:"latency_spike_multiplier"`
	P95Multiplier          float64 `yaml:"p95_multiplier"`
}

// QualityChecksConfig contains quality check configuration.
type QualityChecksConfig struct {
	Enabled        bool            `yaml:"enabled"`
	ControlPrompts []ControlPrompt `yaml:"control_prompts"`
}

// ControlPrompt represents a control prompt for quality checks.
type ControlPrompt struct {
	Name            string `yaml:"name"`
	Prompt          string `yaml:"prompt"`
	ExpectedPattern string `yaml:"expected_pattern"`
	MaxTokens       int    `yaml:"max_tokens"`
}

// TelegramConfig contains Telegram bot configuration.
type TelegramConfig struct {
	Enabled   bool              `yaml:"enabled"`
	BotToken  string            `yaml:"bot_token"`
	ChatID    int64             `yaml:"chat_id"`
	RateLimit TelegramRateLimit `yaml:"rate_limit"`
	Alerts    TelegramAlerts    `yaml:"alerts"`
}

// TelegramRateLimit contains Telegram rate limiting configuration.
type TelegramRateLimit struct {
	MessagesPerMinute int `yaml:"messages_per_minute"`
}

// TelegramAlerts contains Telegram alerts configuration.
type TelegramAlerts struct {
	Thresholds []float64 `yaml:"thresholds"`
}

// AlertsConfig contains alert service configuration.
type AlertsConfig struct {
	// Enabled enables or disables the alert service.
	Enabled bool `yaml:"enabled"`
	// Thresholds defines the quota usage thresholds that trigger alerts.
	// Default: [85.0, 95.0]
	Thresholds []float64 `yaml:"thresholds"`
	// Debounce is the minimum time between duplicate alerts.
	// Default: 30m
	Debounce time.Duration `yaml:"debounce"`
	// DailyDigestEnabled enables daily digest emails.
	DailyDigestEnabled bool `yaml:"daily_digest_enabled"`
	// DailyDigestTime is the time of day to send the digest (format: "HH:MM").
	// Default: "09:00"
	DailyDigestTime string `yaml:"daily_digest_time"`
	// Timezone is the timezone for scheduling.
	// Default: "UTC"
	Timezone string `yaml:"timezone"`
	// RateLimitPerMinute limits the number of alerts per minute.
	// Default: 30
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
	// ShutdownTimeout is the timeout for graceful shutdown.
	// Default: 25s
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// MiddlewareConfig contains middleware client configuration including fail-open settings.
type MiddlewareConfig struct {
	// FailOpenTimeout is the timeout for fail-open behavior.
	// Default is 50ms. If QuotaGuard doesn't respond within this time,
	// the system will fall back to alternative account selection.
	FailOpenTimeout time.Duration `yaml:"fail_open_timeout"`

	// MaxRetries is the maximum number of retries before triggering fail-open.
	// Default is 0 (no retries).
	MaxRetries int `yaml:"max_retries"`

	// RetryBackoff is the backoff duration between retries.
	// Default is 10ms.
	RetryBackoff time.Duration `yaml:"retry_backoff"`

	// EnableMetrics enables collection of fail-open metrics.
	// Default is true.
	EnableMetrics bool `yaml:"enable_metrics"`

	// FallbackStrategy is the strategy to use when QuotaGuard is unavailable.
	// Options: "round-robin" (default), "first-available", "weighted"
	FallbackStrategy string `yaml:"fallback_strategy"`

	// GracefulShutdownTimeout is the timeout for graceful shutdown.
	// Must be less than 30s as per requirements. Default is 25s.
	GracefulShutdownTimeout time.Duration `yaml:"graceful_shutdown_timeout"`
}

// CleanupConfig contains cleanup/retention configuration.
type CleanupConfig struct {
	// Enabled enables or disables the cleanup service.
	Enabled bool `yaml:"enabled"`

	// Interval is the time between cleanup runs.
	// Default: 1h
	Interval time.Duration `yaml:"interval"`

	// SoftDeleteEnabled enables soft delete for critical events.
	// Default: true
	SoftDeleteEnabled bool `yaml:"soft_delete_enabled"`

	// HardDeleteAfter is the time after which soft-deleted records are permanently removed.
	// Default: 30d
	HardDeleteAfter time.Duration `yaml:"hard_delete_after"`

	// VacuumEnabled enables periodic VACUUM operations.
	// Default: true
	VacuumEnabled bool `yaml:"vacuum_enabled"`

	// VacuumInterval is the time between VACUUM operations.
	// Default: 24h
	VacuumInterval time.Duration `yaml:"vacuum_interval"`

	// AnalyzeEnabled enables periodic ANALYZE operations.
	// Default: true
	AnalyzeEnabled bool `yaml:"analyze_enabled"`

	// AnalyzeInterval is the time between ANALYZE operations.
	// Default: 1h
	AnalyzeInterval time.Duration `yaml:"analyze_interval"`

	// ShutdownTimeout is the timeout for graceful shutdown.
	// Default: 30s
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`

	// BatchSize is the number of records to delete per batch.
	// Default: 1000
	BatchSize int `yaml:"batch_size"`
}

// AccountConfig contains account configuration.
type AccountConfig struct {
	ID               string  `yaml:"id"`
	Provider         string  `yaml:"provider"`
	Tier             string  `yaml:"tier"`
	Enabled          bool    `yaml:"enabled"`
	Priority         int     `yaml:"priority"`
	ConcurrencyLimit int     `yaml:"concurrency_limit"`
	InputCost        float64 `yaml:"input_cost_per_1k"`
	OutputCost       float64 `yaml:"output_cost_per_1k"`
	CredentialsRef   string  `yaml:"credentials_ref"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}

	if err := c.Server.Validate(); err != nil {
		return fmt.Errorf("server: %w", err)
	}

	if err := c.API.Validate(); err != nil {
		return fmt.Errorf("api: %w", err)
	}

	if err := c.Collector.Validate(); err != nil {
		return fmt.Errorf("collector: %w", err)
	}

	if err := c.Router.Validate(); err != nil {
		return fmt.Errorf("router: %w", err)
	}

	if err := c.Health.Validate(); err != nil {
		return fmt.Errorf("health: %w", err)
	}

	if err := c.Telegram.Validate(); err != nil {
		return fmt.Errorf("telegram: %w", err)
	}

	if err := c.Alerts.Validate(); err != nil {
		return fmt.Errorf("alerts: %w", err)
	}

	if err := c.Cleanup.Validate(); err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	for i, acc := range c.Accounts {
		if err := acc.Validate(); err != nil {
			return fmt.Errorf("account[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates server configuration.
func (s *ServerConfig) Validate() error {
	if s.Host == "" {
		return fmt.Errorf("host is required")
	}
	if s.HTTPPort <= 0 || s.HTTPPort > 65535 {
		return fmt.Errorf("http_port must be between 1 and 65535")
	}
	if s.ShutdownTimeout < 0 {
		return fmt.Errorf("shutdown_timeout must be positive")
	}
	if s.ShutdownTimeout == 0 {
		s.ShutdownTimeout = 30 * time.Second
	}
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
	if s.LogFormat == "" {
		s.LogFormat = "json"
	}
	// Validate TLS configuration
	if s.TLS.Enabled {
		if s.TLS.CertFile == "" {
			return fmt.Errorf("tls cert_file is required when TLS is enabled")
		}
		if s.TLS.KeyFile == "" {
			return fmt.Errorf("tls key_file is required when TLS is enabled")
		}
		if s.TLS.MinVersion != "" && s.TLS.MinVersion != "1.2" && s.TLS.MinVersion != "1.3" {
			return fmt.Errorf("tls min_version must be either \"1.2\" or \"1.3\"")
		}
		if s.TLS.MinVersion == "" {
			s.TLS.MinVersion = "1.3"
		}
	}
	return nil
}

// Validate validates API configuration.
func (a *APIConfig) Validate() error {
	if a.BasePath == "" {
		a.BasePath = "/api/v1"
	}
	if a.Auth.Enabled && len(a.Auth.APIKeys) == 0 {
		return fmt.Errorf("auth: api_keys is required when auth is enabled")
	}
	if a.RateLimit.RequestsPerMinute <= 0 {
		a.RateLimit.RequestsPerMinute = 1000
	}
	// Cap rate limit to prevent abuse
	if a.RateLimit.RequestsPerMinute > 100000 {
		a.RateLimit.RequestsPerMinute = 100000
	}
	if a.RateLimit.Burst <= 0 {
		a.RateLimit.Burst = 100
	}
	// Cap burst to reasonable value
	if a.RateLimit.Burst > 10000 {
		a.RateLimit.Burst = 10000
	}
	return nil
}

// Validate validates collector configuration.
func (c *CollectorConfig) Validate() error {
	if c.Mode == "" {
		c.Mode = "hybrid"
	}
	if c.Mode != "passive" && c.Mode != "active" && c.Mode != "hybrid" {
		return fmt.Errorf("mode must be one of: passive, active, hybrid")
	}
	if c.Passive.BufferSize <= 0 {
		c.Passive.BufferSize = 1000
	}
	if c.Passive.FlushInterval <= 0 {
		c.Passive.FlushInterval = 2 * time.Second
	}
	if c.Active.DefaultInterval <= 0 {
		c.Active.DefaultInterval = 60 * time.Second
	}
	if c.Active.Timeout <= 0 {
		c.Active.Timeout = 10 * time.Second
	}
	if c.Active.RetryAttempts < 0 {
		return fmt.Errorf("retry_attempts cannot be negative")
	}
	return nil
}

// Validate validates router configuration.
func (r *RouterConfig) Validate() error {
	if r.Thresholds.Warning <= 0 {
		r.Thresholds.Warning = 85.0
	}
	if r.Thresholds.Switch <= 0 {
		r.Thresholds.Switch = 90.0
	}
	if r.Thresholds.Critical <= 0 {
		r.Thresholds.Critical = 95.0
	}
	if r.Thresholds.MinSafe <= 0 {
		r.Thresholds.MinSafe = 5.0
	}
	// Ensure thresholds are in valid range [0, 100]
	if r.Thresholds.Warning > 100 {
		r.Thresholds.Warning = 100
	}
	if r.Thresholds.Switch > 100 {
		r.Thresholds.Switch = 100
	}
	if r.Thresholds.Critical > 100 {
		r.Thresholds.Critical = 100
	}
	// Ensure logical ordering: warning < switch < critical
	if r.Thresholds.Warning >= r.Thresholds.Switch {
		r.Thresholds.Warning = r.Thresholds.Switch - 5
		if r.Thresholds.Warning < 0 {
			r.Thresholds.Warning = 75
			r.Thresholds.Switch = 85
		}
	}
	if r.Thresholds.Switch >= r.Thresholds.Critical {
		r.Thresholds.Critical = r.Thresholds.Switch + 5
		if r.Thresholds.Critical > 100 {
			r.Thresholds.Critical = 100
			r.Thresholds.Switch = 90
		}
	}
	if r.AntiFlapping.MinDwellTime <= 0 {
		r.AntiFlapping.MinDwellTime = 5 * time.Minute
	}
	if r.AntiFlapping.CooldownAfterSwitch <= 0 {
		r.AntiFlapping.CooldownAfterSwitch = 3 * time.Minute
	}
	if r.Reservation.Timeout <= 0 {
		r.Reservation.Timeout = 30 * time.Second
	}
	if r.Reservation.CleanupInterval <= 0 {
		r.Reservation.CleanupInterval = 10 * time.Second
	}
	if r.Reservation.DefaultEstimatedCostPercent <= 0 {
		r.Reservation.DefaultEstimatedCostPercent = 2.0
	}
	// Cap cost percentages
	if r.Reservation.DefaultEstimatedCostPercent > 100 {
		r.Reservation.DefaultEstimatedCostPercent = 100
	}
	return nil
}

// Validate validates health configuration.
func (h *HealthConfig) Validate() error {
	if h.Interval <= 0 {
		h.Interval = 5 * time.Minute
	}
	if h.Timeout <= 0 {
		h.Timeout = 5 * time.Second
	}
	return nil
}

// Validate validates Telegram configuration.
func (t *TelegramConfig) Validate() error {
	if !t.Enabled {
		return nil
	}
	if t.RateLimit.MessagesPerMinute <= 0 {
		t.RateLimit.MessagesPerMinute = 20
	}
	return nil
}

// Validate validates alerts configuration and applies defaults.
func (a *AlertsConfig) Validate() error {
	// Set default thresholds if not provided
	if len(a.Thresholds) == 0 {
		a.Thresholds = []float64{85.0, 95.0}
	}

	// Set default debounce
	if a.Debounce <= 0 {
		a.Debounce = 30 * time.Minute
	}

	// Set default digest time
	if a.DailyDigestTime == "" {
		a.DailyDigestTime = "09:00"
	}

	// Set default timezone
	if a.Timezone == "" {
		a.Timezone = "UTC"
	}

	// Set default rate limit
	if a.RateLimitPerMinute <= 0 {
		a.RateLimitPerMinute = 30
	}

	// Set default shutdown timeout
	if a.ShutdownTimeout <= 0 {
		a.ShutdownTimeout = 25 * time.Second
	}

	return nil
}

// Validate validates account configuration.
func (a *AccountConfig) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("id is required")
	}
	if a.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if a.ConcurrencyLimit < 0 {
		return fmt.Errorf("concurrency_limit cannot be negative")
	}
	if a.InputCost < 0 {
		return fmt.Errorf("input_cost_per_1k cannot be negative")
	}
	if a.OutputCost < 0 {
		return fmt.Errorf("output_cost_per_1k cannot be negative")
	}
	return nil
}

// Validate validates middleware configuration and applies defaults.
func (m *MiddlewareConfig) Validate() error {
	// Apply defaults for zero values
	if m.FailOpenTimeout <= 0 {
		m.FailOpenTimeout = 50 * time.Millisecond
	}

	if m.RetryBackoff <= 0 {
		m.RetryBackoff = 10 * time.Millisecond
	}

	if m.MaxRetries < 0 {
		m.MaxRetries = 0
	}

	if m.GracefulShutdownTimeout <= 0 {
		m.GracefulShutdownTimeout = 25 * time.Second
	}

	// Validate graceful shutdown timeout must be < 30s
	if m.GracefulShutdownTimeout >= 30*time.Second {
		return fmt.Errorf("graceful_shutdown_timeout must be less than 30s")
	}

	// Validate fallback strategy
	validStrategies := map[string]bool{
		"":                true, // empty means use default
		"round-robin":     true,
		"first-available": true,
		"weighted":        true,
	}

	if !validStrategies[m.FallbackStrategy] {
		return fmt.Errorf("invalid fallback_strategy: %s (valid options: round-robin, first-available, weighted)", m.FallbackStrategy)
	}

	return nil
}

// GetFallbackStrategy returns the fallback strategy type as a string.
// Returns "round-robin" if not set.
func (m *MiddlewareConfig) GetFallbackStrategy() string {
	if m.FallbackStrategy == "" {
		return "round-robin"
	}
	return m.FallbackStrategy
}

// Validate validates cleanup configuration and applies defaults.
func (c *CleanupConfig) Validate() error {
	// Set default interval
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}

	// Set default hard delete period
	if c.HardDeleteAfter <= 0 {
		c.HardDeleteAfter = 30 * 24 * time.Hour
	}

	// Set default vacuum interval
	if c.VacuumInterval <= 0 {
		c.VacuumInterval = 24 * time.Hour
	}

	// Set default analyze interval
	if c.AnalyzeInterval <= 0 {
		c.AnalyzeInterval = time.Hour
	}

	// Set default shutdown timeout
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}

	// Set default batch size
	if c.BatchSize <= 0 {
		c.BatchSize = 1000
	}

	return nil
}
