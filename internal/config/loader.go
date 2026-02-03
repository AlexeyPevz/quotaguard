package config

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/errors"
	"gopkg.in/yaml.v3"
)

// Loader handles configuration loading and hot-reloading
type Loader struct {
	path     string
	mu       sync.RWMutex
	config   *Config
	lastMod  time.Time
	onChange func(*Config)
	stopOnce sync.Once
	stopChan chan struct{}
}

// NewLoader creates a new configuration loader
func NewLoader(path string) *Loader {
	return &Loader{
		path:     path,
		stopChan: make(chan struct{}),
	}
}

// Load reads the configuration from the file
func (l *Loader) Load() (*Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, err := os.Stat(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &errors.ErrConfigNotFound{Path: l.path}
		}
		return nil, err
	}

	content, err := os.ReadFile(l.path)
	if err != nil {
		return nil, &errors.ErrFileRead{Path: l.path, Err: err}
	}

	content = substituteEnvVars(content)
	config, err := Parse(content)
	if err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	l.config = config
	l.lastMod = info.ModTime()

	return config, nil
}

// Reload forces a reload of the configuration
func (l *Loader) Reload() (*Config, error) {
	config, err := l.Load()
	if err != nil {
		return nil, err
	}

	l.mu.RLock()
	onChange := l.onChange
	l.mu.RUnlock()

	if onChange != nil {
		onChange(config)
	}

	return config, nil
}

// Get returns the current configuration
func (l *Loader) Get() *Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config
}

// SetOnChange sets a callback to be called when configuration changes
func (l *Loader) SetOnChange(fn func(*Config)) {
	l.mu.Lock()
	l.onChange = fn
	l.mu.Unlock()
}

// StartWatcher starts checking for file changes
func (l *Loader) StartWatcher(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-l.stopChan:
				return
			case <-ticker.C:
				l.checkFileChange()
			}
		}
	}()
}

// StopWatcher stops the file watcher
func (l *Loader) StopWatcher() {
	l.stopOnce.Do(func() {
		close(l.stopChan)
	})
}

func (l *Loader) checkFileChange() {
	info, err := os.Stat(l.path)
	if err != nil {
		return
	}

	l.mu.RLock()
	lastMod := l.lastMod
	l.mu.RUnlock()

	if info.ModTime().After(lastMod) {
		if _, err := l.Reload(); err != nil {
			fmt.Printf("Error reloading config: %v\n", err)
		}
	}
}

// LoadFromEnv loads configuration using path from environment variable or default
func LoadFromEnv() (*Config, error) {
	path := os.Getenv("QUOTAGUARD_CONFIG_PATH")
	if path == "" {
		path = "config.yaml"
	}
	loader := NewLoader(path)
	return loader.Load()
}

// MustLoad loads configuration or panics on error
func MustLoad(path string) *Config {
	loader := NewLoader(path)
	config, err := loader.Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}
	return config
}

// MustLoadFromEnv loads configuration from env or panics
func MustLoadFromEnv() *Config {
	config, err := LoadFromEnv()
	if err != nil {
		panic(fmt.Sprintf("failed to load config from env: %v", err))
	}
	return config
}

// Parse parses configuration from byte slice
func Parse(data []byte) (*Config, error) {
	var config Config

	// Apply defaults before parsing
	config.Server.HTTPPort = 8318
	config.Server.ShutdownTimeout = 30 * time.Second
	config.Server.LogLevel = "info"
	config.Server.LogFormat = "json"

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, &errors.ErrConfigParse{Err: err}
	}

	if err := config.Validate(); err != nil {
		return nil, &errors.ErrConfigValidation{Err: err}
	}

	return &config, nil
}

func substituteEnvVars(content []byte) []byte {
	return []byte(os.ExpandEnv(string(content)))
}

func configsEqual(a, b *Config) bool {
	if a == nil || b == nil {
		return a == b
	}
	// Simple comparison for now, in production might want deep include/ignore fields
	return a.Version == b.Version &&
		a.Server == b.Server
}
