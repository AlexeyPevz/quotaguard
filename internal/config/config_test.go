package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
					LogLevel:        "info",
					LogFormat:       "json",
				},
				API: APIConfig{
					Enabled:  true,
					BasePath: "/api/v1",
					RateLimit: RateLimitConfig{
						RequestsPerMinute: 1000,
						Burst:             100,
					},
				},
				Collector: CollectorConfig{
					Mode: "hybrid",
					Passive: PassiveCollectorConfig{
						Enabled:       true,
						BufferSize:    1000,
						FlushInterval: 2 * time.Second,
					},
					Active: ActiveCollectorConfig{
						Enabled:         true,
						DefaultInterval: 60 * time.Second,
						Adaptive:        true,
						Timeout:         10 * time.Second,
						RetryAttempts:   3,
						RetryBackoff:    "exponential",
						CircuitBreaker: CircuitBreakerConfig{
							FailureThreshold: 3,
							Timeout:          5 * time.Minute,
						},
					},
				},
				Router: RouterConfig{
					Thresholds: ThresholdsConfig{
						Warning:  85.0,
						Switch:   90.0,
						Critical: 95.0,
						MinSafe:  5.0,
					},
					AntiFlapping: AntiFlappingConfig{
						MinDwellTime:        5 * time.Minute,
						CooldownAfterSwitch: 3 * time.Minute,
						HysteresisMargin:    15.0,
					},
					Reservation: ReservationConfig{
						Enabled:                     true,
						Timeout:                     30 * time.Second,
						CleanupInterval:             10 * time.Second,
						DefaultEstimatedCostPercent: 2.0,
					},
					Weights: WeightsConfig{
						Safety:      0.4,
						Refill:      0.3,
						Tier:        0.15,
						Reliability: 0.1,
						Cost:        0.05,
					},
				},
				Health: HealthConfig{
					Enabled:  true,
					Interval: 5 * time.Minute,
					Timeout:  5 * time.Second,
				},
				Telegram: TelegramConfig{
					Enabled:  false,
					BotToken: "",
					ChatID:   0,
				},
			},
			wantErr: false,
		},
		{
			name: "missing version",
			config: Config{
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
				},
			},
			wantErr: true,
			errMsg:  "version is required",
		},
		{
			name: "invalid server host",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
				},
			},
			wantErr: true,
			errMsg:  "server: host is required",
		},
		{
			name: "invalid server port",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        0,
					ShutdownTimeout: 30 * time.Second,
				},
			},
			wantErr: true,
			errMsg:  "server: http_port must be between 1 and 65535",
		},
		{
			name: "invalid collector mode",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
				},
				Collector: CollectorConfig{
					Mode: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "collector: mode must be one of: passive, active, hybrid",
		},
		{
			name: "auth enabled without api keys",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
				},
				API: APIConfig{
					Enabled: true,
					Auth: AuthConfig{
						Enabled: true,
					},
				},
			},
			wantErr: true,
			errMsg:  "auth: api_keys is required when auth is enabled",
		},
		{
			name: "telegram enabled without token",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
				},
				Telegram: TelegramConfig{
					Enabled:  true,
					BotToken: "",
					ChatID:   123,
				},
			},
			wantErr: false,
			errMsg:  "",
		},
		{
			name: "telegram enabled without chat_id",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
				},
				Telegram: TelegramConfig{
					Enabled:  true,
					BotToken: "token",
					ChatID:   0,
				},
			},
			wantErr: false,
			errMsg:  "",
		},
		{
			name: "invalid account",
			config: Config{
				Version: "2.1",
				Server: ServerConfig{
					Host:            "127.0.0.1",
					HTTPPort:        8318,
					ShutdownTimeout: 30 * time.Second,
				},
				Accounts: []AccountConfig{
					{ID: "", Provider: "openai"},
				},
			},
			wantErr: true,
			errMsg:  "account[0]: id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestServerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ServerConfig{
				Host:            "127.0.0.1",
				HTTPPort:        8318,
				ShutdownTimeout: 30 * time.Second,
				LogLevel:        "info",
				LogFormat:       "json",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: ServerConfig{
				HTTPPort:        8318,
				ShutdownTimeout: 30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "port too low",
			config: ServerConfig{
				Host:            "127.0.0.1",
				HTTPPort:        0,
				ShutdownTimeout: 30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "port too high",
			config: ServerConfig{
				Host:            "127.0.0.1",
				HTTPPort:        70000,
				ShutdownTimeout: 30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative shutdown timeout",
			config: ServerConfig{
				Host:            "127.0.0.1",
				HTTPPort:        8318,
				ShutdownTimeout: -1 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "defaults applied",
			config: ServerConfig{
				Host:            "127.0.0.1",
				HTTPPort:        8318,
				ShutdownTimeout: 30 * time.Second,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Check defaults were applied
				if tt.config.LogLevel == "" {
					t.Error("expected LogLevel to have default")
				}
				if tt.config.LogFormat == "" {
					t.Error("expected LogFormat to have default")
				}
			}
		})
	}
}

func TestAPIConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  APIConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: APIConfig{
				Enabled:  true,
				BasePath: "/api/v1",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 1000,
					Burst:             100,
				},
			},
			wantErr: false,
		},
		{
			name: "empty base path gets default",
			config: APIConfig{
				Enabled:  true,
				BasePath: "",
				RateLimit: RateLimitConfig{
					RequestsPerMinute: 1000,
					Burst:             100,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCollectorConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  CollectorConfig
		wantErr bool
	}{
		{
			name: "valid hybrid mode",
			config: CollectorConfig{
				Mode: "hybrid",
			},
			wantErr: false,
		},
		{
			name: "valid passive mode",
			config: CollectorConfig{
				Mode: "passive",
			},
			wantErr: false,
		},
		{
			name: "valid active mode",
			config: CollectorConfig{
				Mode: "active",
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			config: CollectorConfig{
				Mode: "invalid",
			},
			wantErr: true,
		},
		{
			name: "negative retry attempts",
			config: CollectorConfig{
				Mode: "hybrid",
				Active: ActiveCollectorConfig{
					RetryAttempts: -1,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRouterConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  RouterConfig
		wantErr bool
	}{
		{
			name:    "empty config gets defaults",
			config:  RouterConfig{},
			wantErr: false,
		},
		{
			name: "valid config",
			config: RouterConfig{
				Thresholds: ThresholdsConfig{
					Warning:  85.0,
					Switch:   90.0,
					Critical: 95.0,
					MinSafe:  5.0,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHealthConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  HealthConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: HealthConfig{
				Enabled:  true,
				Interval: 5 * time.Minute,
				Timeout:  5 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "empty config gets defaults",
			config: HealthConfig{
				Enabled: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTelegramConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  TelegramConfig
		wantErr bool
	}{
		{
			name: "disabled is valid",
			config: TelegramConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "enabled with all fields",
			config: TelegramConfig{
				Enabled:  true,
				BotToken: "test-token",
				ChatID:   123456,
			},
			wantErr: false,
		},
		{
			name: "enabled without token",
			config: TelegramConfig{
				Enabled:  true,
				BotToken: "",
				ChatID:   123456,
			},
			wantErr: false,
		},
		{
			name: "enabled without chat_id",
			config: TelegramConfig{
				Enabled:  true,
				BotToken: "test-token",
				ChatID:   0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAccountConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  AccountConfig
		wantErr bool
	}{
		{
			name: "valid account",
			config: AccountConfig{
				ID:               "acc-1",
				Provider:         "openai",
				Tier:             "tier-1",
				Enabled:          true,
				Priority:         1,
				ConcurrencyLimit: 10,
				InputCost:        0.01,
				OutputCost:       0.03,
			},
			wantErr: false,
		},
		{
			name: "missing id",
			config: AccountConfig{
				Provider: "openai",
			},
			wantErr: true,
		},
		{
			name: "missing provider",
			config: AccountConfig{
				ID: "acc-1",
			},
			wantErr: true,
		},
		{
			name: "negative concurrency limit",
			config: AccountConfig{
				ID:               "acc-1",
				Provider:         "openai",
				ConcurrencyLimit: -1,
			},
			wantErr: true,
		},
		{
			name: "negative input cost",
			config: AccountConfig{
				ID:        "acc-1",
				Provider:  "openai",
				InputCost: -0.01,
			},
			wantErr: true,
		},
		{
			name: "negative output cost",
			config: AccountConfig{
				ID:         "acc-1",
				Provider:   "openai",
				OutputCost: -0.03,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSubstituteEnvVars(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_VAR", "test_value")
	os.Setenv("ANOTHER_VAR", "another_value")
	defer func() {
		os.Unsetenv("TEST_VAR")
		os.Unsetenv("ANOTHER_VAR")
	}()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no substitution",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "single substitution",
			input:    "value is ${TEST_VAR}",
			expected: "value is test_value",
		},
		{
			name:     "multiple substitutions",
			input:    "${TEST_VAR} and ${ANOTHER_VAR}",
			expected: "test_value and another_value",
		},
		{
			name:     "missing env var returns empty",
			input:    "value is ${MISSING_VAR}",
			expected: "value is ",
		},
		{
			name:     "mixed content",
			input:    "prefix ${TEST_VAR} middle ${ANOTHER_VAR} suffix",
			expected: "prefix test_value middle another_value suffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := substituteEnvVars([]byte(tt.input))
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestLoad(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "info"
  log_format: "json"
api:
  enabled: true
  base_path: "/api/v1"
  auth:
    enabled: false
    type: "bearer"
    secret: "${TEST_SECRET}"
  rate_limit:
    requests_per_minute: 1000
    burst: 100
`

	// Set environment variable
	os.Setenv("TEST_SECRET", "my-secret-value")
	defer os.Unsetenv("TEST_SECRET")

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Test loading
	loader := NewLoader(configPath)
	config, err := loader.Load()
	require.NoError(t, err)

	assert.Equal(t, "2.1", config.Version)
	assert.Equal(t, "127.0.0.1", config.Server.Host)
	assert.Equal(t, 8318, config.Server.HTTPPort)
	assert.Equal(t, "/api/v1", config.API.BasePath)
	assert.Equal(t, "my-secret-value", config.API.Auth.Secret)
}

func TestLoad_FileNotFound(t *testing.T) {
	loader := NewLoader("/nonexistent/path/config.yaml")
	_, err := loader.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file not found")
}

func TestParse(t *testing.T) {
	configYAML := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "debug"
  log_format: "json"
collector:
  mode: "hybrid"
  passive:
    enabled: true
    buffer_size: 1000
    flush_interval: "2s"
  active:
    enabled: true
    default_interval: "60s"
    adaptive: true
    timeout: "10s"
    retry_attempts: 3
    retry_backoff: "exponential"
    circuit_breaker:
      failure_threshold: 3
      timeout: "5m"
router:
  thresholds:
    warning: 85.0
    switch: 90.0
    critical: 95.0
    min_safe: 5.0
  anti_flapping:
    min_dwell_time: "5m"
    cooldown_after_switch: "3m"
    hysteresis_margin: 15.0
  reservation:
    enabled: true
    timeout: "30s"
    cleanup_interval: "10s"
    default_estimated_cost_percent: 2.0
  weights:
    safety: 0.4
    refill: 0.3
    tier: 0.15
    reliability: 0.1
    cost: 0.05
  policies:
    - name: "balanced"
      weights:
        safety: 0.4
        refill: 0.3
        tier: 0.15
        reliability: 0.1
        cost: 0.05
  fallback_chains:
    anthropic:
      - "openai"
      - "gemini"
health:
  enabled: true
  interval: "5m"
  timeout: "5s"
telegram:
  enabled: false
`

	config, err := Parse([]byte(configYAML))
	require.NoError(t, err)

	assert.Equal(t, "2.1", config.Version)
	assert.Equal(t, "127.0.0.1", config.Server.Host)
	assert.Equal(t, 8318, config.Server.HTTPPort)
	assert.Equal(t, "debug", config.Server.LogLevel)
	assert.Equal(t, "hybrid", config.Collector.Mode)
	assert.Equal(t, 1000, config.Collector.Passive.BufferSize)
	assert.Equal(t, 2*time.Second, config.Collector.Passive.FlushInterval)
	assert.Equal(t, 60*time.Second, config.Collector.Active.DefaultInterval)
	assert.True(t, config.Collector.Active.Adaptive)
	assert.Equal(t, 85.0, config.Router.Thresholds.Warning)
	assert.Equal(t, 90.0, config.Router.Thresholds.Switch)
	assert.Equal(t, 5*time.Minute, config.Router.AntiFlapping.MinDwellTime)
	assert.Equal(t, 30*time.Second, config.Router.Reservation.Timeout)
	assert.Equal(t, 0.4, config.Router.Weights.Safety)
	assert.Len(t, config.Router.Policies, 1)
	assert.Equal(t, "balanced", config.Router.Policies[0].Name)
	assert.Len(t, config.Router.FallbackChains["anthropic"], 2)
	assert.Equal(t, 5*time.Minute, config.Health.Interval)
	assert.False(t, config.Telegram.Enabled)
}

func TestParse_InvalidYAML(t *testing.T) {
	invalidYAML := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: not_a_number
`

	_, err := Parse([]byte(invalidYAML))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestParse_InvalidConfig(t *testing.T) {
	invalidConfig := `
version: ""
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
`

	_, err := Parse([]byte(invalidConfig))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestLoader(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "info"
  log_format: "json"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	loader := NewLoader(configPath)

	// Test Load
	config, err := loader.Load()
	require.NoError(t, err)
	assert.Equal(t, "2.1", config.Version)

	// Test Get
	gotConfig := loader.Get()
	assert.Equal(t, config, gotConfig)

	// Test Reload
	newConfig, err := loader.Reload()
	require.NoError(t, err)
	assert.Equal(t, "2.1", newConfig.Version)
}

func TestLoader_OnChange(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "info"
  log_format: "json"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	loader := NewLoader(configPath)

	changeCalled := false
	loader.SetOnChange(func(c *Config) {
		changeCalled = true
	})

	// Initial load
	_, err = loader.Load()
	require.NoError(t, err)

	// Reload should trigger onChange
	_, err = loader.Reload()
	require.NoError(t, err)
	assert.True(t, changeCalled)
}

func TestLoadFromEnv(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "info"
  log_format: "json"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set environment variable
	os.Setenv("QUOTAGUARD_CONFIG_PATH", configPath)
	defer os.Unsetenv("QUOTAGUARD_CONFIG_PATH")

	config, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "2.1", config.Version)
}

func TestLoadFromEnv_DefaultPath(t *testing.T) {
	// Create a temporary config file in current directory
	configContent := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "info"
  log_format: "json"
`

	// Save current directory and change to temp
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		require.NoError(t, os.Chdir(originalDir))
	}()

	err = os.WriteFile("config.yaml", []byte(configContent), 0644)
	require.NoError(t, err)

	config, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "2.1", config.Version)
}

func TestLoadFromEnv_NotFound(t *testing.T) {
	// Ensure no config file exists
	os.Unsetenv("QUOTAGUARD_CONFIG_PATH")

	// Change to temp directory without config
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		require.NoError(t, os.Chdir(originalDir))
	}()

	_, err = LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file not found")
}

func TestMustLoad(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "info"
  log_format: "json"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Should not panic
	config := MustLoad(configPath)
	assert.Equal(t, "2.1", config.Version)
}

func TestMustLoad_Panic(t *testing.T) {
	// Should panic on invalid path
	assert.Panics(t, func() {
		MustLoad("/nonexistent/path/config.yaml")
	})
}

func TestMustLoadFromEnv(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
  shutdown_timeout: "30s"
  log_level: "info"
  log_format: "json"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	os.Setenv("QUOTAGUARD_CONFIG_PATH", configPath)
	defer os.Unsetenv("QUOTAGUARD_CONFIG_PATH")

	// Should not panic
	config := MustLoadFromEnv()
	assert.Equal(t, "2.1", config.Version)
}

func TestMustLoadFromEnv_Panic(t *testing.T) {
	os.Unsetenv("QUOTAGUARD_CONFIG_PATH")

	// Change to temp directory without config
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		require.NoError(t, os.Chdir(originalDir))
	}()

	// Should panic when config not found
	assert.Panics(t, func() {
		MustLoadFromEnv()
	})
}

func TestConfigsEqual(t *testing.T) {
	config1 := &Config{
		Version: "2.1",
		Server: ServerConfig{
			Host:            "127.0.0.1",
			HTTPPort:        8318,
			ShutdownTimeout: 30 * time.Second,
			LogLevel:        "info",
			LogFormat:       "json",
		},
	}

	config2 := &Config{
		Version: "2.1",
		Server: ServerConfig{
			Host:            "127.0.0.1",
			HTTPPort:        8318,
			ShutdownTimeout: 30 * time.Second,
			LogLevel:        "info",
			LogFormat:       "json",
		},
	}

	config3 := &Config{
		Version: "2.0",
		Server: ServerConfig{
			Host:            "127.0.0.1",
			HTTPPort:        8318,
			ShutdownTimeout: 30 * time.Second,
			LogLevel:        "info",
			LogFormat:       "json",
		},
	}

	assert.True(t, configsEqual(config1, config2))
	assert.False(t, configsEqual(config1, config3))
	assert.True(t, configsEqual(nil, nil))
	assert.False(t, configsEqual(config1, nil))
	assert.False(t, configsEqual(nil, config1))
}
