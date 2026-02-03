package limiter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

func TestLimiter_AcquireRelease(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 3,
	})

	l := New(s, nil)

	// Should acquire up to limit
	for i := 0; i < 3; i++ {
		if !l.Acquire("acc1") {
			t.Errorf("Acquire %d: expected true, got false", i+1)
		}
	}

	// Should fail at limit
	if l.Acquire("acc1") {
		t.Error("Acquire at limit: expected false, got true")
	}

	// Release and re-acquire
	l.Release("acc1")
	if !l.Acquire("acc1") {
		t.Error("Re-acquire after release: expected true")
	}

	// Cleanup
	for i := 0; i < 3; i++ {
		l.Release("acc1")
	}
}

func TestLimiter_NoLimit(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 0, // No limit
	})

	l := New(s, nil)

	// Should always succeed
	for i := 0; i < 100; i++ {
		if !l.Acquire("acc1") {
			t.Errorf("Acquire %d: expected true with no limit", i)
		}
	}
}

func TestLimiter_Concurrent(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 10,
	})

	l := New(s, nil)
	var wg sync.WaitGroup
	successes := make(chan bool, 100)

	// 100 goroutines trying to acquire
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Retry until we acquire a slot
			for {
				if l.Acquire("acc1") {
					successes <- true
					time.Sleep(10 * time.Millisecond)
					l.Release("acc1")
					return
				}
				// Brief backoff before retry
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	close(successes)

	totalSuccess := 0
	for ok := range successes {
		if ok {
			totalSuccess++
		}
	}

	// All should eventually succeed
	if totalSuccess != 100 {
		t.Errorf("Expected 100 successes, got %d", totalSuccess)
	}

	// Final count should be 0
	if l.GetCurrent("acc1") != 0 {
		t.Errorf("Expected 0 final count, got %d", l.GetCurrent("acc1"))
	}
}

func TestLimiter_UpdateLimit(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 2,
	})

	l := New(s, nil)

	// Acquire 2
	l.Acquire("acc1")
	l.Acquire("acc1")

	// Should fail
	if l.Acquire("acc1") {
		t.Error("Expected false at limit 2")
	}

	// Update limit
	l.UpdateLimit("acc1", 5)

	// Should now succeed
	if !l.Acquire("acc1") {
		t.Error("Expected true after limit increase")
	}

	// Cleanup
	for i := 0; i < 3; i++ {
		l.Release("acc1")
	}
}

func TestWaiter_Acquire(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 1,
	})

	l := New(s, nil)

	// Take the only slot
	if !l.Acquire("acc1") {
		t.Fatal("Failed to acquire initial slot")
	}

	// Try to wait for slot with timeout
	waiter := l.NewWaiter("acc1", 50*time.Millisecond)
	ctx := context.Background()

	// Should timeout
	if err := waiter.Acquire(ctx); err == nil {
		t.Error("Expected timeout error")
	}

	// Release and try again
	l.Release("acc1")

	waiter2 := l.NewWaiter("acc1", 1*time.Second)
	if err := waiter2.Acquire(ctx); err != nil {
		t.Errorf("Expected success, got %v", err)
	}

	l.Release("acc1")
}

func TestLimiter_UnknownAccount(t *testing.T) {
	s := store.NewMemoryStore()
	l := New(s, nil)

	// Unknown account should return true (no limit)
	if !l.Acquire("unknown") {
		t.Error("Expected true for unknown account")
	}

	l.Release("unknown") // Should not panic
}

func TestLimiter_GetCurrent(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 5,
	})

	l := New(s, nil)

	// Unknown account
	if l.GetCurrent("unknown") != 0 {
		t.Error("Expected 0 for unknown account")
	}

	// After acquire
	l.Acquire("acc1")
	if l.GetCurrent("acc1") != 1 {
		t.Errorf("Expected 1, got %d", l.GetCurrent("acc1"))
	}

	l.Release("acc1")
	if l.GetCurrent("acc1") != 0 {
		t.Errorf("Expected 0 after release, got %d", l.GetCurrent("acc1"))
	}
}

func TestLimiter_NegativeLimit(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: -1,
	})

	l := New(s, nil)

	// Negative limit should be treated as no limit
	if !l.Acquire("acc1") {
		t.Error("Expected true with negative limit (treated as unlimited)")
	}
}

func TestWaiter_ContextCancel(t *testing.T) {
	s := store.NewMemoryStore()
	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 1,
	})

	l := New(s, nil)
	l.Acquire("acc1") // Take the slot

	ctx, cancel := context.WithCancel(context.Background())
	waiter := l.NewWaiter("acc1", 5*time.Second)

	// Cancel context
	cancel()

	if err := waiter.Acquire(ctx); err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}

	l.Release("acc1")
}
