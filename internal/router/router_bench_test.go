package router

import (
	"context"
	"fmt"
	"testing"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// BenchmarkRouterSelect benchmarks the main account selection path.
func BenchmarkRouterSelect(b *testing.B) {
	b.ReportAllocs()

	// Setup
	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	// Create test accounts
	for i := 0; i < 100; i++ {
		acc := &models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			Priority:         100 - i%10,
			ConcurrencyLimit: 10,
			InputCost:        0.01,
			OutputCost:       0.03,
		}
		s.SetAccount(acc)
		s.SetQuota(fmt.Sprintf("account-%d", i), &models.QuotaInfo{
			AccountID:             fmt.Sprintf("account-%d", i),
			EffectiveRemainingPct: float64(100 - i%20),
			Confidence:            0.9,
			Provider:              models.ProviderOpenAI,
		})
	}

	b.ResetTimer()

	// Benchmark
	for i := 0; i < b.N; i++ {
		_, _ = r.Select(context.Background(), SelectRequest{})
	}
}

// BenchmarkRouterSelectMultipleAccounts benchmarks selection with multiple accounts and providers.
func BenchmarkRouterSelectMultipleAccounts(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	providers := []models.Provider{
		models.ProviderOpenAI,
		models.ProviderAnthropic,
		models.ProviderGemini,
		models.ProviderAzure,
	}

	// Create 50 accounts per provider
	for p, provider := range providers {
		for i := 0; i < 50; i++ {
			acc := &models.Account{
				ID:               fmt.Sprintf("account-%s-%d", provider, i),
				Provider:         provider,
				Enabled:          true,
				Priority:         100 - i%10,
				ConcurrencyLimit: 10,
			}
			s.SetAccount(acc)
			s.SetQuota(fmt.Sprintf("account-%s-%d", provider, i), &models.QuotaInfo{
				AccountID:             fmt.Sprintf("account-%s-%d", provider, i),
				EffectiveRemainingPct: float64(100 - (i+p)%30),
				Confidence:            0.8,
				Provider:              provider,
			})
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = r.Select(context.Background(), SelectRequest{})
	}
}

// BenchmarkRouterSelectWithFiltering benchmarks selection with provider filtering.
func BenchmarkRouterSelectWithFiltering(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	// Create mixed provider accounts
	for i := 0; i < 200; i++ {
		provider := models.ProviderOpenAI
		switch i % 4 {
		case 0:
			provider = models.ProviderOpenAI
		case 1:
			provider = models.ProviderAnthropic
		case 2:
			provider = models.ProviderGemini
		case 3:
			provider = models.ProviderAzure
		}

		acc := &models.Account{
			ID:       fmt.Sprintf("account-%d", i),
			Provider: provider,
			Enabled:  true,
			Priority: 100 - i%10,
		}
		s.SetAccount(acc)
		s.SetQuota(fmt.Sprintf("account-%d", i), &models.QuotaInfo{
			AccountID:             fmt.Sprintf("account-%d", i),
			EffectiveRemainingPct: float64(100 - i%20),
			Confidence:            0.9,
			Provider:              provider,
		})
	}

	b.ResetTimer()

	// Benchmark with filtering
	for i := 0; i < b.N; i++ {
		_, _ = r.Select(context.Background(), SelectRequest{
			Provider: models.ProviderOpenAI,
		})
	}
}

// BenchmarkRouterScoreAccount benchmarks individual account scoring.
func BenchmarkRouterScoreAccount(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	// Create a single account
	acc := &models.Account{
		ID:               "test-account",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		Priority:         100,
		ConcurrencyLimit: 10,
		InputCost:        0.01,
		OutputCost:       0.03,
	}
	s.SetAccount(acc)
	s.SetQuota("test-account", &models.QuotaInfo{
		AccountID:             "test-account",
		EffectiveRemainingPct: 75.0,
		Confidence:            0.9,
		Provider:              models.ProviderOpenAI,
	})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = r.Select(context.Background(), SelectRequest{})
	}
}

// BenchmarkRouterAntiFlapping benchmarks anti-flapping logic during rapid selections.
func BenchmarkRouterAntiFlapping(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	// Reduce dwell time for testing
	cfg.MinDwellTime = 0
	cfg.HysteresisMargin = 0
	r := NewRouter(s, cfg)

	// Create two accounts with different scores
	for i := 0; i < 2; i++ {
		acc := &models.Account{
			ID:       fmt.Sprintf("account-%d", i),
			Provider: models.ProviderOpenAI,
			Enabled:  true,
			Priority: 100 - i,
		}
		s.SetAccount(acc)
		s.SetQuota(fmt.Sprintf("account-%d", i), &models.QuotaInfo{
			AccountID:             fmt.Sprintf("account-%d", i),
			EffectiveRemainingPct: float64(100 - i*50), // Account 0: 100%, Account 1: 50%
			Confidence:            0.9,
			Provider:              models.ProviderOpenAI,
		})
	}

	// Reset timer and benchmark rapid selections
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = r.Select(context.Background(), SelectRequest{})
	}
}

// BenchmarkCircuitBreaker benchmarks circuit breaker operations.
func BenchmarkCircuitBreaker(b *testing.B) {
	b.ReportAllocs()

	cb := NewCircuitBreaker("test", 5, 30)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = cb.Execute(context.Background(), func() error {
			return nil
		})
	}
}

// BenchmarkCircuitBreakerWithFailures benchmarks circuit breaker with failures.
func BenchmarkCircuitBreakerWithFailures(b *testing.B) {
	b.ReportAllocs()

	cb := NewCircuitBreaker("test", 10, 30)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := cb.Execute(context.Background(), func() error {
			if i%3 == 0 {
				return fmt.Errorf("error")
			}
			return nil
		})
		_ = err
	}
}

// BenchmarkRouterSelectWithExclusions benchmarks selection with account exclusions.
func BenchmarkRouterSelectWithExclusions(b *testing.B) {
	b.ReportAllocs()

	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	// Create 50 accounts
	for i := 0; i < 50; i++ {
		acc := &models.Account{
			ID:       fmt.Sprintf("account-%d", i),
			Provider: models.ProviderOpenAI,
			Enabled:  true,
			Priority: 100 - i%10,
		}
		s.SetAccount(acc)
		s.SetQuota(fmt.Sprintf("account-%d", i), &models.QuotaInfo{
			AccountID:             fmt.Sprintf("account-%d", i),
			EffectiveRemainingPct: float64(100 - i%20),
			Confidence:            0.9,
			Provider:              models.ProviderOpenAI,
		})
	}

	// Prepare exclusions (10 accounts to exclude)
	excluded := make([]string, 10)
	for i := 0; i < 10; i++ {
		excluded[i] = fmt.Sprintf("account-%d", i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = r.Select(context.Background(), SelectRequest{
			Exclude: excluded,
		})
	}
}
