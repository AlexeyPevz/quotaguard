package store

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTestSettingsStore(t *testing.T) (*SQLiteSettingsStore, func()) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	store, err := NewSQLiteSettingsStore(db)
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
	}

	return store, cleanup
}

func TestSettingsStore_GetSet(t *testing.T) {
	store, cleanup := newTestSettingsStore(t)
	defer cleanup()

	// Test Set and Get
	err := store.Set("test_key", "test_value")
	require.NoError(t, err)

	value, ok := store.Get("test_key")
	assert.True(t, ok)
	assert.Equal(t, "test_value", value)

	// Test Get non-existent key
	_, ok = store.Get("non_existent")
	assert.False(t, ok)
}

func TestSettingsStore_Update(t *testing.T) {
	store, cleanup := newTestSettingsStore(t)
	defer cleanup()

	// Set initial value
	err := store.Set("update_key", "value1")
	require.NoError(t, err)

	// Update value
	err = store.Set("update_key", "value2")
	require.NoError(t, err)

	value, ok := store.Get("update_key")
	assert.True(t, ok)
	assert.Equal(t, "value2", value)
}

func TestSettingsStore_Delete(t *testing.T) {
	store, cleanup := newTestSettingsStore(t)
	defer cleanup()

	// Set and then delete
	err := store.Set("delete_key", "value")
	require.NoError(t, err)

	err = store.Delete("delete_key")
	require.NoError(t, err)

	_, ok := store.Get("delete_key")
	assert.False(t, ok)
}

func TestSettingsStore_GetInt(t *testing.T) {
	store, cleanup := newTestSettingsStore(t)
	defer cleanup()

	// Default value for non-existent key
	result := store.GetInt("non_existent", 42)
	assert.Equal(t, 42, result)

	// Set integer value
	err := store.SetInt("int_key", 100)
	require.NoError(t, err)

	result = store.GetInt("int_key", 0)
	assert.Equal(t, 100, result)

	// Invalid value returns default
	require.NoError(t, store.Set("invalid_int", "not_a_number"))
	result = store.GetInt("invalid_int", 99)
	assert.Equal(t, 99, result)
}

func TestSettingsStore_GetFloat(t *testing.T) {
	store, cleanup := newTestSettingsStore(t)
	defer cleanup()

	// Default value for non-existent key
	result := store.GetFloat("non_existent", 3.14)
	assert.Equal(t, 3.14, result)

	// Set float value
	err := store.SetFloat("float_key", 2.718)
	require.NoError(t, err)

	result = store.GetFloat("float_key", 0)
	assert.Equal(t, 2.718, result)

	// Invalid value returns default
	require.NoError(t, store.Set("invalid_float", "not_a_float"))
	result = store.GetFloat("invalid_float", 1.0)
	assert.Equal(t, 1.0, result)
}

func TestSettingsStore_GetBool(t *testing.T) {
	store, cleanup := newTestSettingsStore(t)
	defer cleanup()

	// Default value for non-existent key
	result := store.GetBool("non_existent", true)
	assert.True(t, result)

	result = store.GetBool("non_existent", false)
	assert.False(t, result)

	// Test true values
	err := store.SetBool("bool_key", true)
	require.NoError(t, err)

	assert.True(t, store.GetBool("bool_key", false))

	// Test false values
	err = store.SetBool("bool_key2", false)
	require.NoError(t, err)

	assert.False(t, store.GetBool("bool_key2", true))
}

func TestSettingsStore_Concurrency(t *testing.T) {
	store, cleanup := newTestSettingsStore(t)
	defer cleanup()

	// Test concurrent access
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			key := "concurrent_key"
			err := store.SetInt(key, idx)
			if err == nil {
				store.GetInt(key, 0)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSettingsStore_Persistence(t *testing.T) {
	// Create a file-based database
	path := "/tmp/test_settings.db"
	os.Remove(path) // Clean up

	db1, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	store1, err := NewSQLiteSettingsStore(db1)
	require.NoError(t, err)

	// Set value
	err = store1.Set("persistent_key", "persistent_value")
	require.NoError(t, err)

	db1.Close()

	// Reopen database
	db2, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	store2, err := NewSQLiteSettingsStore(db2)
	require.NoError(t, err)

	// Value should persist
	value, ok := store2.Get("persistent_key")
	assert.True(t, ok)
	assert.Equal(t, "persistent_value", value)

	db2.Close()
	os.Remove(path) // Clean up
}
