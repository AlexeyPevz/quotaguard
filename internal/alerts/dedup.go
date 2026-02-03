package alerts

import (
	"sync"
	"time"
)

// DedupStore stores sent alerts for deduplication
type DedupStore struct {
	records map[string]*AlertRecord
	window  time.Duration
	mu      sync.RWMutex
}

// NewDedupStore creates a new deduplication store
func NewDedupStore(window time.Duration) *DedupStore {
	if window <= 0 {
		window = 30 * time.Minute
	}
	return &DedupStore{
		records: make(map[string]*AlertRecord),
		window:  window,
	}
}

// IsDuplicate checks if an alert is a duplicate
func (d *DedupStore) IsDuplicate(key string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	record, exists := d.records[key]
	if !exists {
		return false
	}

	// Check if the record is still within the dedup window
	if time.Since(record.SentAt) < d.window {
		return true
	}

	return false
}

// Record records a sent alert
func (d *DedupStore) Record(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if record, exists := d.records[key]; exists {
		record.SentAt = time.Now()
		record.Count++
	} else {
		d.records[key] = &AlertRecord{
			AlertKey: key,
			SentAt:   time.Now(),
			Count:    1,
		}
	}
}

// GetRecord gets the record for a key
func (d *DedupStore) GetRecord(key string) *AlertRecord {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.records[key]
}

// Cleanup removes old records
func (d *DedupStore) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for key, record := range d.records {
		if now.Sub(record.SentAt) > d.window {
			delete(d.records, key)
		}
	}
}

// Size returns the number of records
func (d *DedupStore) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return len(d.records)
}

// Clear clears all records
func (d *DedupStore) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.records = make(map[string]*AlertRecord)
}
