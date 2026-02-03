package limiter

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quotaguard/quotaguard/internal/metrics"
	"github.com/quotaguard/quotaguard/internal/store"
)

// Limiter manages per-account concurrency limits.
type Limiter struct {
	store   store.Store
	metrics *metrics.Metrics
	limits  map[string]int64  // accountID -> limit
	current map[string]*int64 // accountID -> atomic counter
	mu      sync.RWMutex
}

// New creates a new concurrency limiter.
func New(s store.Store, m *metrics.Metrics) *Limiter {
	return &Limiter{
		store:   s,
		metrics: m,
		limits:  make(map[string]int64),
		current: make(map[string]*int64),
	}
}

// Acquire attempts to acquire a slot for the given account.
// Returns true if acquired, false if limit reached.
func (l *Limiter) Acquire(accountID string) bool {
	l.mu.RLock()
	limit, ok := l.limits[accountID]
	counterPtr, hasCounter := l.current[accountID]
	l.mu.RUnlock()

	if !ok {
		acc, found := l.store.GetAccount(accountID)
		if !found || acc.ConcurrencyLimit <= 0 {
			if l.metrics != nil {
				l.metrics.RecordLimiterAcquire("success")
			}
			return true
		}
		l.mu.Lock()
		limit = int64(acc.ConcurrencyLimit)
		l.limits[accountID] = limit
		counter := int64(0)
		l.current[accountID] = &counter
		counterPtr = &counter
		if l.metrics != nil {
			l.metrics.SetLimiterCapacity(accountID, acc.ConcurrencyLimit)
			l.metrics.SetLimiterTokensAvailable(accountID, 0)
		}
		l.mu.Unlock()
	}

	if !hasCounter {
		l.mu.Lock()
		counter := int64(0)
		l.current[accountID] = &counter
		counterPtr = &counter
		l.mu.Unlock()
	}

	for {
		current := atomic.LoadInt64(counterPtr)
		if current >= limit {
			if l.metrics != nil {
				l.metrics.RecordLimiterAcquire("denied")
			}
			return false
		}
		if atomic.CompareAndSwapInt64(counterPtr, current, current+1) {
			if l.metrics != nil {
				l.metrics.RecordLimiterAcquire("success")
				l.metrics.SetLimiterTokensAvailable(accountID, int(limit-current-1))
			}
			return true
		}
	}
}

// Release releases a slot for the given account.
func (l *Limiter) Release(accountID string) {
	l.mu.RLock()
	counterPtr, ok := l.current[accountID]
	limit, hasLimit := l.limits[accountID]
	l.mu.RUnlock()
	if !ok {
		return
	}

	for {
		current := atomic.LoadInt64(counterPtr)
		if current <= 0 {
			return
		}
		if atomic.CompareAndSwapInt64(counterPtr, current, current-1) {
			if l.metrics != nil {
				l.metrics.RecordLimiterRelease()
				if hasLimit {
					l.metrics.SetLimiterTokensAvailable(accountID, int(limit-current+1))
				}
			}
			return
		}
	}
}

// GetCurrent returns current concurrent count for an account.
func (l *Limiter) GetCurrent(accountID string) int64 {
	l.mu.RLock()
	counterPtr, ok := l.current[accountID]
	l.mu.RUnlock()
	if !ok {
		return 0
	}
	return atomic.LoadInt64(counterPtr)
}

// UpdateLimit updates the limit for an account (e.g., from hot reload).
func (l *Limiter) UpdateLimit(accountID string, limit int) {
	if limit < 0 {
		limit = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits[accountID] = int64(limit)
	if l.metrics != nil {
		l.metrics.SetLimiterCapacity(accountID, limit)
	}
}

// Waiter provides blocking acquisition with timeout.
type Waiter struct {
	limiter      *Limiter
	accountID    string
	timeout      time.Duration
	pollInterval time.Duration
}

// NewWaiter creates a waiter for blocking acquisition.
func (l *Limiter) NewWaiter(accountID string, timeout time.Duration) *Waiter {
	return &Waiter{
		limiter:      l,
		accountID:    accountID,
		timeout:      timeout,
		pollInterval: 10 * time.Millisecond,
	}
}

// Acquire blocks until a slot is available or timeout.
func (w *Waiter) Acquire(ctx context.Context) error {
	start := time.Now()
	deadline := time.Now().Add(w.timeout)
	for {
		if w.limiter.Acquire(w.accountID) {
			if w.limiter.metrics != nil {
				w.limiter.metrics.RecordLimiterWaitDuration("success", time.Since(start).Seconds())
			}
			return nil
		}
		if time.Now().After(deadline) {
			if w.limiter.metrics != nil {
				w.limiter.metrics.RecordLimiterWaitDuration("timeout", time.Since(start).Seconds())
			}
			return fmt.Errorf("timeout waiting for concurrency slot")
		}
		select {
		case <-ctx.Done():
			if w.limiter.metrics != nil {
				w.limiter.metrics.RecordLimiterWaitDuration("timeout", time.Since(start).Seconds())
			}
			return ctx.Err()
		case <-time.After(w.pollInterval):
			continue
		}
	}
}
