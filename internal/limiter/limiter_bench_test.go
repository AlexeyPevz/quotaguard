package limiter

import (
	"fmt"
	"testing"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// BenchmarkLimiterAcquire benchmarks slot acquisition.
func BenchmarkLimiterAcquire(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	l := New(s, nil)

	// Set up accounts with limits
	for i := 0; i < 100; i++ {
		s.SetAccount(&models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			ConcurrencyLimit: 10,
		})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = l.Acquire(fmt.Sprintf("account-%d", i%100))
	}
}

// BenchmarkLimiterRelease benchmarks slot release.
func BenchmarkLimiterRelease(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	l := New(s, nil)

	// Set up accounts with limits
	for i := 0; i < 100; i++ {
		s.SetAccount(&models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			ConcurrencyLimit: 10,
		})
		// Acquire first so we can release
		l.Acquire(fmt.Sprintf("account-%d", i))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		l.Release(fmt.Sprintf("account-%d", i%100))
	}
}

// BenchmarkLimiterConcurrent benchmarks concurrent acquire/release operations.
func BenchmarkLimiterConcurrent(b *testing.B) {
	b.ReportAllocs()
	b.SetParallelism(10)

	s := store.NewMemoryStore()
	l := New(s, nil)

	// Set up accounts with limits
	for i := 0; i < 50; i++ {
		s.SetAccount(&models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			ConcurrencyLimit: 100, // Higher limit for concurrent testing
		})
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			accountID := fmt.Sprintf("account-%d", syncConnIndex%50)
			syncConnIndex++

			if syncConnIndex%2 == 0 {
				l.Acquire(accountID)
			} else {
				l.Release(accountID)
			}
		}
	})
}

var syncConnIndex int64

// BenchmarkLimiterAcquireNoLimit benchmarks acquire when no limit is set.
func BenchmarkLimiterAcquireNoLimit(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	l := New(s, nil)

	// Create account without concurrency limit
	s.SetAccount(&models.Account{
		ID:               "no-limit-account",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 0, // No limit
	})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = l.Acquire("no-limit-account")
	}
}

// BenchmarkLimiterAcquireAtLimit benchmarks acquire when at limit.
func BenchmarkLimiterAcquireAtLimit(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	l := New(s, nil)

	// Set up account with limit of 1
	s.SetAccount(&models.Account{
		ID:               "limited-account",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 1,
	})

	// Acquire the only slot
	l.Acquire("limited-account")

	b.ResetTimer()

	// This should fail all the time
	for i := 0; i < b.N; i++ {
		_ = l.Acquire("limited-account")
	}
}

// BenchmarkLimiterAcquireReleaseCycle benchmarks full acquire/release cycle.
func BenchmarkLimiterAcquireReleaseCycle(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	l := New(s, nil)

	s.SetAccount(&models.Account{
		ID:               "cycle-account",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 10,
	})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if l.Acquire("cycle-account") {
			l.Release("cycle-account")
		}
	}
}

// BenchmarkLimiterManyAccounts benchmarks operations across many accounts.
func BenchmarkLimiterManyAccounts(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	l := New(s, nil)

	// Set up many accounts
	for i := 0; i < 1000; i++ {
		s.SetAccount(&models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			ConcurrencyLimit: 5,
		})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = l.Acquire(fmt.Sprintf("account-%d", i%1000))
	}
}

// BenchmarkLimiterUpdateLimit benchmarks limit updates.
func BenchmarkLimiterUpdateLimit(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	l := New(s, nil)

	s.SetAccount(&models.Account{
		ID:               "update-account",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		ConcurrencyLimit: 10,
	})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		l.UpdateLimit("update-account", (i%20)+1)
	}
}
