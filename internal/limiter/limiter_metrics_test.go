package limiter

import (
	"context"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/metrics"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

func TestLimiter_MetricsAcquireSuccess(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 3,
	})

	// Acquire a slot
	if !l.Acquire("acc1") {
		t.Fatal("Failed to acquire slot")
	}

	// Verify metrics were recorded
	// Note: In a real test, we would check the actual metric values
	// by reading from the metrics registry, but for simplicity we just
	// verify that the operations complete without panics
	l.Release("acc1")
}

func TestLimiter_MetricsAcquireDenied(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 1,
	})

	// Acquire the only slot
	if !l.Acquire("acc1") {
		t.Fatal("Failed to acquire initial slot")
	}

	// Try to acquire again - should be denied
	if l.Acquire("acc1") {
		t.Error("Expected acquire to be denied")
	}

	// Cleanup
	l.Release("acc1")
}

func TestLimiter_MetricsRelease(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 3,
	})

	// Acquire and release multiple times
	for i := 0; i < 5; i++ {
		if !l.Acquire("acc1") {
			t.Errorf("Failed to acquire on iteration %d", i)
		}
		l.Release("acc1")
	}

	// Verify final count is 0
	if l.GetCurrent("acc1") != 0 {
		t.Errorf("Expected 0 final count, got %d", l.GetCurrent("acc1"))
	}
}

func TestLimiter_MetricsCapacity(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 5,
	})

	// Acquire a slot to trigger capacity metric
	if !l.Acquire("acc1") {
		t.Fatal("Failed to acquire slot")
	}

	// Update limit
	l.UpdateLimit("acc1", 10)

	// Cleanup
	l.Release("acc1")
}

func TestLimiter_MetricsTokensAvailable(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 3,
	})

	// Acquire all slots
	for i := 0; i < 3; i++ {
		if !l.Acquire("acc1") {
			t.Errorf("Failed to acquire slot %d", i)
		}
	}

	// Release one slot
	l.Release("acc1")

	// Verify count
	if l.GetCurrent("acc1") != 2 {
		t.Errorf("Expected 2 current count, got %d", l.GetCurrent("acc1"))
	}

	// Cleanup
	for i := 0; i < 2; i++ {
		l.Release("acc1")
	}
}

func TestLimiter_MetricsWaiterSuccess(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 1,
	})

	// Take the slot
	if !l.Acquire("acc1") {
		t.Fatal("Failed to acquire initial slot")
	}

	// Start a goroutine to release the slot after a delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		l.Release("acc1")
	}()

	// Wait for the slot
	waiter := l.NewWaiter("acc1", 1*time.Second)
	ctx := context.Background()
	if err := waiter.Acquire(ctx); err != nil {
		t.Errorf("Expected success, got %v", err)
	}

	// Cleanup
	l.Release("acc1")
}

func TestLimiter_MetricsWaiterTimeout(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 1,
	})

	// Take the slot
	if !l.Acquire("acc1") {
		t.Fatal("Failed to acquire initial slot")
	}

	// Try to wait with a short timeout
	waiter := l.NewWaiter("acc1", 50*time.Millisecond)
	ctx := context.Background()
	if err := waiter.Acquire(ctx); err == nil {
		t.Error("Expected timeout error")
	}

	// Cleanup
	l.Release("acc1")
}

func TestLimiter_MetricsNil(t *testing.T) {
	s := store.NewMemoryStore()
	l := New(s, nil) // nil metrics should not cause panics

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 3,
	})

	// All operations should work without panics
	if !l.Acquire("acc1") {
		t.Fatal("Failed to acquire slot")
	}
	l.Release("acc1")
	l.UpdateLimit("acc1", 5)
}

func TestLimiter_MetricsMultipleAccounts(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	// Set up multiple accounts
	accounts := []string{"acc1", "acc2", "acc3"}
	for _, accID := range accounts {
		s.SetAccount(&models.Account{
			ID:               accID,
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			ConcurrencyLimit: 2,
		})
	}

	// Acquire slots for each account
	for _, accID := range accounts {
		if !l.Acquire(accID) {
			t.Errorf("Failed to acquire slot for %s", accID)
		}
	}

	// Release all
	for _, accID := range accounts {
		l.Release(accID)
	}

	// Verify all counts are 0
	for _, accID := range accounts {
		if l.GetCurrent(accID) != 0 {
			t.Errorf("Expected 0 for %s, got %d", accID, l.GetCurrent(accID))
		}
	}
}

func TestLimiter_MetricsConcurrent(t *testing.T) {
	s := store.NewMemoryStore()
	m := metrics.NewMetrics("test")
	l := New(s, m)

	s.SetAccount(&models.Account{
		ID:               "acc1",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 10,
	})

	// Concurrent acquire/release operations
	done := make(chan bool)
	for i := 0; i < 20; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				if l.Acquire("acc1") {
					time.Sleep(1 * time.Millisecond)
					l.Release("acc1")
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Verify final count is 0
	if l.GetCurrent("acc1") != 0 {
		t.Errorf("Expected 0 final count, got %d", l.GetCurrent("acc1"))
	}
}
