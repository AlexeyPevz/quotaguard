package alerts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewDedupStore(t *testing.T) {
	store := NewDedupStore(30 * time.Minute)
	assert.NotNil(t, store)
	assert.NotNil(t, store.records)
	assert.Equal(t, 30*time.Minute, store.window)
}

func TestNewDedupStoreDefault(t *testing.T) {
	store := NewDedupStore(0)
	assert.NotNil(t, store)
	assert.Equal(t, 30*time.Minute, store.window)
}

func TestIsDuplicate(t *testing.T) {
	store := NewDedupStore(100 * time.Millisecond)

	key := "test:key"

	// Initially not a duplicate
	assert.False(t, store.IsDuplicate(key))

	// Record the key
	store.Record(key)

	// Now it should be a duplicate
	assert.True(t, store.IsDuplicate(key))

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should not be a duplicate anymore
	assert.False(t, store.IsDuplicate(key))
}

func TestRecord(t *testing.T) {
	store := NewDedupStore(30 * time.Minute)

	key := "test:key"

	// Record first time
	store.Record(key)
	record := store.GetRecord(key)
	assert.NotNil(t, record)
	assert.Equal(t, 1, record.Count)

	// Record again
	store.Record(key)
	record = store.GetRecord(key)
	assert.Equal(t, 2, record.Count)
}

func TestGetRecordNotFound(t *testing.T) {
	store := NewDedupStore(30 * time.Minute)

	record := store.GetRecord("nonexistent:key")
	assert.Nil(t, record)
}

func TestCleanup(t *testing.T) {
	store := NewDedupStore(50 * time.Millisecond)

	// Add some records
	store.Record("key1")
	store.Record("key2")

	assert.Equal(t, 2, store.Size())

	// Wait for window to expire
	time.Sleep(100 * time.Millisecond)

	// Cleanup
	store.Cleanup()

	// Should be empty
	assert.Equal(t, 0, store.Size())
}

func TestCleanupPartial(t *testing.T) {
	store := NewDedupStore(100 * time.Millisecond)

	// Add first record
	store.Record("key1")

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Add second record
	store.Record("key2")

	assert.Equal(t, 2, store.Size())

	// Wait for first to expire but not second
	time.Sleep(60 * time.Millisecond)

	// Cleanup
	store.Cleanup()

	// Should have only key2
	assert.Equal(t, 1, store.Size())
	assert.Nil(t, store.GetRecord("key1"))
	assert.NotNil(t, store.GetRecord("key2"))
}

func TestSize(t *testing.T) {
	store := NewDedupStore(30 * time.Minute)

	assert.Equal(t, 0, store.Size())

	store.Record("key1")
	assert.Equal(t, 1, store.Size())

	store.Record("key2")
	assert.Equal(t, 2, store.Size())

	store.Record("key1") // Duplicate
	assert.Equal(t, 2, store.Size())
}

func TestClear(t *testing.T) {
	store := NewDedupStore(30 * time.Minute)

	store.Record("key1")
	store.Record("key2")
	assert.Equal(t, 2, store.Size())

	store.Clear()
	assert.Equal(t, 0, store.Size())
	assert.Nil(t, store.GetRecord("key1"))
	assert.Nil(t, store.GetRecord("key2"))
}

func TestDedupConcurrency(t *testing.T) {
	store := NewDedupStore(30 * time.Minute)

	// Run concurrent operations
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			key := "key"
			store.IsDuplicate(key)
			store.Record(key)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have exactly one record
	assert.Equal(t, 1, store.Size())
	record := store.GetRecord("key")
	assert.Equal(t, 10, record.Count)
}
