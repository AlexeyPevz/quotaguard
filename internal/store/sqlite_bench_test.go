package store

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

func setupTempSQLiteDB(b *testing.B) (*SQLiteStore, func()) {
	tmpDir := b.TempDir()
	dbPath := fmt.Sprintf("%s/bench.db", tmpDir)

	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		b.Fatalf("failed to create SQLite store: %v", err)
	}

	cleanup := func() {
		s.Close()
		os.Remove(dbPath)
	}

	return s, cleanup
}

// BenchmarkSQLiteGetAccount benchmarks account retrieval.
func BenchmarkSQLiteGetAccount(b *testing.B) {
	b.ReportAllocs()

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	// Pre-populate with test accounts
	for i := 0; i < 100; i++ {
		acc := &models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			Priority:         100 - i%10,
			ConcurrencyLimit: 10,
			InputCost:        0.01,
			OutputCost:       0.03,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.SetAccount(acc)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = s.GetAccount(fmt.Sprintf("account-%d", i%100))
	}
}

// BenchmarkSQLiteSetAccount benchmarks account storage.
func BenchmarkSQLiteSetAccount(b *testing.B) {
	b.ReportAllocs()

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	acc := &models.Account{
		ID:               "test-account",
		Provider:         models.ProviderOpenAI,
		Enabled:          true,
		Priority:         100,
		ConcurrencyLimit: 10,
		InputCost:        0.01,
		OutputCost:       0.03,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		acc.ID = fmt.Sprintf("account-%d", i)
		s.SetAccount(acc)
	}
}

// BenchmarkSQLiteListAccounts benchmarks listing all accounts.
func BenchmarkSQLiteListAccounts(b *testing.B) {
	b.ReportAllocs()

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	// Pre-populate with test accounts
	for i := 0; i < 500; i++ {
		acc := &models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          i%2 == 0,
			Priority:         100 - i%10,
			ConcurrencyLimit: 10,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.SetAccount(acc)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = s.ListAccounts()
	}
}

// BenchmarkSQLiteGetQuota benchmarks quota retrieval.
func BenchmarkSQLiteGetQuota(b *testing.B) {
	b.ReportAllocs()

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	// Pre-populate with test quotas
	for i := 0; i < 100; i++ {
		quota := &models.QuotaInfo{
			AccountID:             fmt.Sprintf("account-%d", i),
			EffectiveRemainingPct: float64(100 - i%20),
			Confidence:            0.9,
			Provider:              models.ProviderOpenAI,
			CollectedAt:           time.Now(),
		}
		s.SetQuota(fmt.Sprintf("account-%d", i), quota)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = s.GetQuota(fmt.Sprintf("account-%d", i%100))
	}
}

// BenchmarkSQLiteSetQuota benchmarks quota storage.
func BenchmarkSQLiteSetQuota(b *testing.B) {
	b.ReportAllocs()

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	quota := &models.QuotaInfo{
		AccountID:             "test-account",
		EffectiveRemainingPct: 75.0,
		Confidence:            0.9,
		Provider:              models.ProviderOpenAI,
		CollectedAt:           time.Now(),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		quota.AccountID = fmt.Sprintf("account-%d", i)
		s.SetQuota(fmt.Sprintf("account-%d", i), quota)
	}
}

// BenchmarkSQLiteConcurrentReads benchmarks concurrent read operations.
func BenchmarkSQLiteConcurrentReads(b *testing.B) {
	b.ReportAllocs()
	b.SetParallelism(10)

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	// Pre-populate with test data
	for i := 0; i < 100; i++ {
		acc := &models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          true,
			Priority:         100 - i%10,
			ConcurrencyLimit: 10,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.SetAccount(acc)
		s.SetQuota(fmt.Sprintf("account-%d", i), &models.QuotaInfo{
			AccountID:             fmt.Sprintf("account-%d", i),
			EffectiveRemainingPct: float64(100 - i%20),
			Confidence:            0.9,
			Provider:              models.ProviderOpenAI,
			CollectedAt:           time.Now(),
		})
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := time.Now().UnixNano() % 100
			_, _ = s.GetAccount(fmt.Sprintf("account-%d", idx))
			_, _ = s.GetQuota(fmt.Sprintf("account-%d", idx))
		}
	})
}

// BenchmarkSQLiteConcurrentWrites benchmarks concurrent write operations.
func BenchmarkSQLiteConcurrentWrites(b *testing.B) {
	b.ReportAllocs()
	b.SetParallelism(10)

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		counter := int64(0)
		for pb.Next() {
			idx := counter
			atomic.AddInt64(&counter, 1)

			acc := &models.Account{
				ID:               fmt.Sprintf("account-%d", idx),
				Provider:         models.ProviderOpenAI,
				Enabled:          true,
				Priority:         int(100 - idx%10),
				ConcurrencyLimit: 10,
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			}
			s.SetAccount(acc)
			s.SetQuota(fmt.Sprintf("account-%d", idx), &models.QuotaInfo{
				AccountID:             fmt.Sprintf("account-%d", idx),
				EffectiveRemainingPct: float64(100 - idx%20),
				Confidence:            0.9,
				Provider:              models.ProviderOpenAI,
				CollectedAt:           time.Now(),
			})
		}
	})
}

// BenchmarkSQLiteListEnabledAccounts benchmarks listing only enabled accounts.
func BenchmarkSQLiteListEnabledAccounts(b *testing.B) {
	b.ReportAllocs()

	s, cleanup := setupTempSQLiteDB(b)
	defer cleanup()

	// Pre-populate with mixed enabled/disabled accounts
	for i := 0; i < 500; i++ {
		acc := &models.Account{
			ID:               fmt.Sprintf("account-%d", i),
			Provider:         models.ProviderOpenAI,
			Enabled:          i%3 != 0, // 2/3 enabled
			Priority:         100 - i%10,
			ConcurrencyLimit: 10,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		s.SetAccount(acc)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = s.ListEnabledAccounts()
	}
}
