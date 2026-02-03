package store

import (
	"fmt"
	"sync"
)

// MemorySettingsStore implements SettingsStore using an in-memory map.
type MemorySettingsStore struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewMemorySettingsStore creates a new in-memory settings store.
func NewMemorySettingsStore() *MemorySettingsStore {
	return &MemorySettingsStore{
		values: make(map[string]string),
	}
}

// Get retrieves a setting value.
func (m *MemorySettingsStore) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.values[key]
	return value, ok
}

// Set sets a setting value.
func (m *MemorySettingsStore) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.values[key] = value
	return nil
}

// Delete removes a setting.
func (m *MemorySettingsStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.values, key)
	return nil
}

// GetInt retrieves an integer setting.
func (m *MemorySettingsStore) GetInt(key string, defaultVal int) int {
	value, ok := m.Get(key)
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

// SetInt sets an integer setting.
func (m *MemorySettingsStore) SetInt(key string, value int) error {
	return m.Set(key, fmt.Sprintf("%d", value))
}

// GetFloat retrieves a float setting.
func (m *MemorySettingsStore) GetFloat(key string, defaultVal float64) float64 {
	value, ok := m.Get(key)
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

// SetFloat sets a float setting.
func (m *MemorySettingsStore) SetFloat(key string, value float64) error {
	return m.Set(key, fmt.Sprintf("%f", value))
}

// GetBool retrieves a bool setting.
func (m *MemorySettingsStore) GetBool(key string, defaultVal bool) bool {
	value, ok := m.Get(key)
	if !ok {
		return defaultVal
	}
	return value == "true" || value == "1" || value == "yes"
}

// SetBool sets a bool setting.
func (m *MemorySettingsStore) SetBool(key string, value bool) error {
	if value {
		return m.Set(key, "true")
	}
	return m.Set(key, "false")
}

// Clear removes all settings.
func (m *MemorySettingsStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values = make(map[string]string)
}

var _ SettingsStore = (*MemorySettingsStore)(nil)
