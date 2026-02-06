package router

import (
	"context"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultWeights(t *testing.T) {
	w := DefaultWeights()
	assert.Equal(t, 0.4, w.Safety)
	assert.Equal(t, 0.3, w.Refill)
	assert.Equal(t, 0.15, w.Tier)
	assert.Equal(t, 0.1, w.Reliability)
	assert.Equal(t, 0.05, w.Cost)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 85.0, cfg.WarningThreshold)
	assert.Equal(t, 90.0, cfg.SwitchThreshold)
	assert.Equal(t, 95.0, cfg.CriticalThreshold)
	assert.Equal(t, 5.0, cfg.MinSafeThreshold)
	assert.Equal(t, 5*time.Minute, cfg.MinDwellTime)
	assert.Equal(t, 3*time.Minute, cfg.CooldownAfterSwitch)
	assert.Equal(t, 5.0, cfg.HysteresisMargin)
	assert.Equal(t, "balanced", cfg.DefaultPolicy)
	assert.Len(t, cfg.Policies, 4)
}

func TestNewRouter(t *testing.T) {
	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	require.NotNil(t, r)

	// Cast to concrete type to access internal fields
	routerImpl := r.(*router)
	assert.Equal(t, s, routerImpl.store)
	assert.Equal(t, cfg, routerImpl.config)
	assert.NotNil(t, routerImpl.lastSwitch)
}

func TestRouter_Select(t *testing.T) {
	s := store.NewMemoryStore()

	// Add test accounts
	acc1 := &models.Account{
		ID:         "acc-1",
		Provider:   models.ProviderOpenAI,
		Tier:       "tier-1",
		Enabled:    true,
		Priority:   5,
		InputCost:  0.01,
		OutputCost: 0.03,
	}
	acc2 := &models.Account{
		ID:         "acc-2",
		Provider:   models.ProviderOpenAI,
		Tier:       "tier-2",
		Enabled:    true,
		Priority:   3,
		InputCost:  0.02,
		OutputCost: 0.06,
	}
	s.SetAccount(acc1)
	s.SetAccount(acc2)

	// Add quota data
	quota1 := &models.QuotaInfo{
		AccountID:             "acc-1",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 80.0,
		Confidence:            1.0,
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 1000, Used: 200, Remaining: 800, RefillRate: 0.5},
		},
	}
	quota2 := &models.QuotaInfo{
		AccountID:             "acc-2",
		Provider:              models.ProviderOpenAI,
		EffectiveRemainingPct: 50.0,
		Confidence:            0.9,
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 1000, Used: 500, Remaining: 500, RefillRate: 0.3},
		},
	}
	s.SetQuota("acc-1", quota1)
	s.SetQuota("acc-2", quota2)

	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	t.Run("select best account", func(t *testing.T) {
		req := SelectRequest{
			Provider: models.ProviderOpenAI,
		}

		resp, err := r.Select(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "acc-1", resp.AccountID) // acc-1 has higher score
		assert.Greater(t, resp.Score, 0.0)
		assert.NotEmpty(t, resp.Reason)
	})

	t.Run("filter by provider", func(t *testing.T) {
		req := SelectRequest{
			Provider: models.ProviderAnthropic,
		}

		_, err := r.Select(context.Background(), req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no suitable accounts found")
	})

	t.Run("exclude accounts", func(t *testing.T) {
		req := SelectRequest{
			Provider: models.ProviderOpenAI,
			Exclude:  []string{"acc-1"},
		}

		resp, err := r.Select(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "acc-2", resp.AccountID)
	})

	t.Run("fallback chain when current account is critical", func(t *testing.T) {
		s2 := store.NewMemoryStore()
		accA := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true, Priority: 5}
		accB := &models.Account{ID: "acc-2", Provider: models.ProviderOpenAI, Enabled: true, Priority: 4}
		accC := &models.Account{ID: "acc-3", Provider: models.ProviderOpenAI, Enabled: true, Priority: 3}
		s2.SetAccount(accA)
		s2.SetAccount(accB)
		s2.SetAccount(accC)

		s2.SetQuota("acc-1", &models.QuotaInfo{AccountID: "acc-1", EffectiveRemainingPct: 2.0})
		s2.SetQuota("acc-2", &models.QuotaInfo{AccountID: "acc-2", EffectiveRemainingPct: 80.0})
		s2.SetQuota("acc-3", &models.QuotaInfo{AccountID: "acc-3", EffectiveRemainingPct: 60.0})

		cfg2 := DefaultConfig()
		cfg2.FallbackChains = map[string][]string{
			"acc-1": {"acc-3"},
		}
		r2 := NewRouter(s2, cfg2).(*router)
		r2.RecordSwitch("acc-1")

		resp, err := r2.Select(context.Background(), SelectRequest{Provider: models.ProviderOpenAI})
		require.NoError(t, err)
		assert.Equal(t, "acc-3", resp.AccountID)
		assert.Contains(t, resp.Reason, "fallback chain")
	})

	t.Run("fallback chain with model normalization", func(t *testing.T) {
		s2 := store.NewMemoryStore()
		accA := &models.Account{ID: "acc-a", Provider: models.ProviderOpenAI, Enabled: true, Priority: 5}
		accB := &models.Account{ID: "acc-b", Provider: models.ProviderOpenAI, Enabled: true, Priority: 4}
		accC := &models.Account{ID: "acc-c", Provider: models.ProviderOpenAI, Enabled: true, Priority: 3}
		s2.SetAccount(accA)
		s2.SetAccount(accB)
		s2.SetAccount(accC)
		s2.SetQuota("acc-a", &models.QuotaInfo{AccountID: "acc-a", EffectiveRemainingPct: 2.0})
		s2.SetQuota("acc-b", &models.QuotaInfo{AccountID: "acc-b", EffectiveRemainingPct: 80.0})
		s2.SetQuota("acc-c", &models.QuotaInfo{AccountID: "acc-c", EffectiveRemainingPct: 60.0})

		cfg2 := DefaultConfig()
		cfg2.FallbackChains = map[string][]string{
			"gpt-5.1": {"acc-b"},
		}
		r2 := NewRouter(s2, cfg2).(*router)
		r2.RecordSwitch("acc-a")

		resp, err := r2.Select(context.Background(), SelectRequest{Provider: models.ProviderOpenAI, Model: "models/gpt-5.1"})
		require.NoError(t, err)
		assert.Equal(t, "acc-b", resp.AccountID)
		assert.Contains(t, resp.Reason, "fallback chain")
	})

	t.Run("no enabled accounts", func(t *testing.T) {
		emptyStore := store.NewMemoryStore()
		emptyRouter := NewRouter(emptyStore, cfg)

		_, err := emptyRouter.Select(context.Background(), SelectRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no enabled accounts")
	})
}

func TestRouter_IgnoresEstimatedQuota(t *testing.T) {
	s := store.NewMemoryStore()
	acc := &models.Account{
		ID:       "acc-est",
		Provider: models.ProviderOpenAI,
		Enabled:  true,
		Priority: 5,
	}
	s.SetAccount(acc)

	quota := &models.QuotaInfo{
		AccountID: "acc-est",
		Provider:  models.ProviderOpenAI,
		Source:    models.SourceEstimated,
		Dimensions: models.DimensionSlice{
			{
				Type:       models.DimensionRPD,
				Limit:      100,
				Used:       0,
				Remaining:  100,
				Semantics:  models.WindowFixed,
				Source:     models.SourceEstimated,
				Confidence: 0.2,
			},
		},
		Confidence: 0.2,
	}
	quota.UpdateEffective()
	s.SetQuota("acc-est", quota)

	cfg := DefaultConfig()
	cfg.IgnoreEstimated = true
	r := NewRouter(s, cfg).(*router)

	score, reason := r.scoreAccount(acc, cfg.Weights, SelectRequest{}, false)
	assert.Equal(t, 0.0, score)
	assert.Equal(t, "estimated quota ignored", reason)
}

func TestRouter_scoreAccount(t *testing.T) {
	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)
	routerImpl := r.(*router)

	acc := &models.Account{
		ID:         "acc-1",
		Provider:   models.ProviderOpenAI,
		Priority:   5,
		InputCost:  0.01,
		OutputCost: 0.03,
	}

	t.Run("no quota data", func(t *testing.T) {
		score, reason := routerImpl.scoreAccount(acc, DefaultWeights(), SelectRequest{}, false)
		assert.Equal(t, 0.0, score)
		assert.Equal(t, "no quota data", reason)
	})

	t.Run("exhausted quota", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID: "acc-1",
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 100, Used: 100, Remaining: 0},
			},
		}
		s.SetQuota("acc-1", quota)

		score, reason := routerImpl.scoreAccount(acc, DefaultWeights(), SelectRequest{}, false)
		assert.Equal(t, 0.0, score)
		assert.Equal(t, "quota exhausted", reason)
	})

	t.Run("critical quota", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "acc-1",
			EffectiveRemainingPct: 3.0, // Below critical threshold
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 100, Used: 97, Remaining: 3},
			},
		}
		s.SetQuota("acc-1", quota)

		score, reason := routerImpl.scoreAccount(acc, DefaultWeights(), SelectRequest{}, false)
		assert.Equal(t, 0.1, score)
		assert.Equal(t, "critical quota level", reason)
	})

	t.Run("above switch threshold when not global low", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "acc-1",
			EffectiveRemainingPct: 6.0, // Used 94%, above switch threshold (90) but below critical (95)
		}
		s.SetQuota("acc-1", quota)

		score, reason := routerImpl.scoreAccount(acc, DefaultWeights(), SelectRequest{}, false)
		assert.Equal(t, 0.0, score)
		assert.Equal(t, "usage above switch threshold", reason)
	})

	t.Run("allow above switch threshold when global low", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "acc-1",
			EffectiveRemainingPct: 6.0,
		}
		s.SetQuota("acc-1", quota)

		score, reason := routerImpl.scoreAccount(acc, DefaultWeights(), SelectRequest{}, true)
		assert.Greater(t, score, 0.0)
		assert.Contains(t, reason, "safety=")
	})

	t.Run("normal scoring", func(t *testing.T) {
		// Use fresh store for this test
		s2 := store.NewMemoryStore()
		r2 := NewRouter(s2, cfg)
		routerImpl2 := r2.(*router)
		acc2 := &models.Account{
			ID:         "acc-1",
			Provider:   models.ProviderOpenAI,
			Priority:   5,
			InputCost:  0.01,
			OutputCost: 0.03,
		}
		quota := &models.QuotaInfo{
			AccountID:             "acc-1",
			EffectiveRemainingPct: 80.0,
			Confidence:            0.95,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 1000, Used: 200, Remaining: 800, RefillRate: 0.5},
			},
		}
		s2.SetQuota("acc-1", quota)

		score, reason := routerImpl2.scoreAccount(acc2, DefaultWeights(), SelectRequest{}, false)
		assert.Greater(t, score, 0.0)
		assert.Contains(t, reason, "safety=")
	})

	t.Run("account with insufficient quota for estimated cost", func(t *testing.T) {
		// Use fresh store for this test
		s2 := store.NewMemoryStore()
		r2 := NewRouter(s2, cfg)
		routerImpl2 := r2.(*router)
		acc2 := &models.Account{
			ID:         "acc-1",
			Provider:   models.ProviderOpenAI,
			Priority:   5,
			InputCost:  0.01,
			OutputCost: 0.03,
		}
		quota := &models.QuotaInfo{
			AccountID:             "acc-1",
			EffectiveRemainingPct: 50.0,
			Confidence:            0.95,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 1000, Used: 500, Remaining: 500, RefillRate: 0.5},
			},
		}
		s2.SetQuota("acc-1", quota)

		// Request requires more cost than available (EstimatedCost > effectiveRemaining - MinSafeThreshold)
		req := SelectRequest{
			EstimatedCost: 50.0, // Requesting 50%, but only 50% remaining and MinSafeThreshold is 5%
		}
		score, reason := routerImpl2.scoreAccount(acc2, DefaultWeights(), req, false)
		assert.Equal(t, 0.2, score)
		assert.Equal(t, "insufficient quota for estimated cost", reason)
	})

	t.Run("account with missing required dimension", func(t *testing.T) {
		// Use fresh store for this test
		s2 := store.NewMemoryStore()
		r2 := NewRouter(s2, cfg)
		routerImpl2 := r2.(*router)
		acc2 := &models.Account{
			ID:         "acc-1",
			Provider:   models.ProviderOpenAI,
			Priority:   5,
			InputCost:  0.01,
			OutputCost: 0.03,
		}
		quota := &models.QuotaInfo{
			AccountID:             "acc-1",
			EffectiveRemainingPct: 80.0,
			Confidence:            0.95,
			Dimensions: models.DimensionSlice{
				{Type: models.DimensionRPM, Limit: 1000, Used: 200, Remaining: 800, RefillRate: 0.5},
			},
		}
		s2.SetQuota("acc-1", quota)

		// Request requires TPM dimension which is not present
		req := SelectRequest{
			RequiredDims: []models.DimensionType{models.DimensionTPM},
		}
		score, reason := routerImpl2.scoreAccount(acc2, DefaultWeights(), req, false)
		assert.Equal(t, 0.0, score)
		assert.Contains(t, reason, "missing required dimension")
	})
}

func TestRouter_canSwitch(t *testing.T) {
	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	cfg.CooldownAfterSwitch = 100 * time.Millisecond
	r := NewRouter(s, cfg)
	routerImpl := r.(*router)

	t.Run("never switched", func(t *testing.T) {
		assert.True(t, routerImpl.canSwitch("acc-1"))
	})

	t.Run("during cooldown", func(t *testing.T) {
		routerImpl.RecordSwitch("acc-2")
		assert.False(t, routerImpl.canSwitch("acc-2"))
	})

	t.Run("after cooldown", func(t *testing.T) {
		routerImpl.RecordSwitch("acc-3")
		time.Sleep(150 * time.Millisecond)
		assert.True(t, routerImpl.canSwitch("acc-3"))
	})
}

func TestRouter_RecordSwitch(t *testing.T) {
	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)

	r.RecordSwitch("acc-1")

	stats := r.GetStats()
	assert.Equal(t, 1, stats.LastSwitches)
}

func TestRouter_getWeights(t *testing.T) {
	s := store.NewMemoryStore()
	cfg := DefaultConfig()
	r := NewRouter(s, cfg)
	routerImpl := r.(*router)

	t.Run("default policy", func(t *testing.T) {
		w := routerImpl.getWeights("")
		assert.Equal(t, DefaultWeights(), w)
	})

	t.Run("balanced policy", func(t *testing.T) {
		w := routerImpl.getWeights("balanced")
		assert.Equal(t, DefaultWeights(), w)
	})

	t.Run("cost policy", func(t *testing.T) {
		w := routerImpl.getWeights("cost")
		assert.Greater(t, w.Cost, w.Safety)
	})

	t.Run("unknown policy", func(t *testing.T) {
		w := routerImpl.getWeights("unknown")
		assert.Equal(t, DefaultWeights(), w)
	})
}

func TestRouter_IsHealthy(t *testing.T) {
	t.Run("healthy with accounts", func(t *testing.T) {
		s := store.NewMemoryStore()
		s.SetAccount(&models.Account{ID: "acc-1", Enabled: true})

		r := NewRouter(s, DefaultConfig())
		assert.True(t, r.IsHealthy())
	})

	t.Run("unhealthy without accounts", func(t *testing.T) {
		s := store.NewMemoryStore()
		r := NewRouter(s, DefaultConfig())
		assert.False(t, r.IsHealthy())
	})
}

func TestRouter_GetAccountStatus(t *testing.T) {
	s := store.NewMemoryStore()

	acc := &models.Account{
		ID:       "acc-1",
		Provider: models.ProviderOpenAI,
		Tier:     "tier-1",
		Enabled:  true,
	}
	s.SetAccount(acc)

	quota := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 75.0,
		VirtualUsedPercent:    5.0,
		Dimensions: models.DimensionSlice{
			{Type: models.DimensionRPM, Limit: 1000, Used: 250, Remaining: 750},
		},
	}
	s.SetQuota("acc-1", quota)

	r := NewRouter(s, DefaultConfig())

	t.Run("existing account", func(t *testing.T) {
		status, err := r.GetAccountStatus("acc-1")
		require.NoError(t, err)
		assert.Equal(t, "acc-1", status.AccountID)
		assert.Equal(t, models.ProviderOpenAI, status.Provider)
		assert.True(t, status.Enabled)
		assert.True(t, status.HasQuotaData)
		assert.Equal(t, 70.0, status.EffectiveRemaining)
	})

	t.Run("non-existent account", func(t *testing.T) {
		_, err := r.GetAccountStatus("non-existent")
		assert.Error(t, err)
	})
}

func TestRouter_CalculateOptimalDistribution(t *testing.T) {
	s := store.NewMemoryStore()

	// Add test accounts with different scores
	acc1 := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true, Priority: 5}
	acc2 := &models.Account{ID: "acc-2", Provider: models.ProviderOpenAI, Enabled: true, Priority: 3}
	s.SetAccount(acc1)
	s.SetAccount(acc2)

	quota1 := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 90.0,
		Confidence:            1.0,
		Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 100, Remaining: 900}},
	}
	quota2 := &models.QuotaInfo{
		AccountID:             "acc-2",
		EffectiveRemainingPct: 50.0,
		Confidence:            0.9,
		Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 500, Remaining: 500}},
	}
	s.SetQuota("acc-1", quota1)
	s.SetQuota("acc-2", quota2)

	r := NewRouter(s, DefaultConfig())

	distribution := r.CalculateOptimalDistribution(context.Background(), 100)
	require.NotNil(t, distribution)
	assert.Len(t, distribution, 2)

	// acc-1 should have higher or equal percentage due to better score (could be equal if both score 0)
	assert.GreaterOrEqual(t, distribution["acc-1"], distribution["acc-2"])

	// Total should be 100%
	var total float64
	for _, pct := range distribution {
		total += pct
	}
	assert.InDelta(t, 100.0, total, 0.01)
}

func TestFilterByProvider(t *testing.T) {
	accounts := []*models.Account{
		{ID: "acc-1", Provider: models.ProviderOpenAI},
		{ID: "acc-2", Provider: models.ProviderAnthropic},
		{ID: "acc-3", Provider: models.ProviderOpenAI},
	}

	filtered := filterByProvider(accounts, models.ProviderOpenAI)
	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-1", filtered[0].ID)
	assert.Equal(t, "acc-3", filtered[1].ID)
}

func TestFilterExcluded(t *testing.T) {
	accounts := []*models.Account{
		{ID: "acc-1"},
		{ID: "acc-2"},
		{ID: "acc-3"},
	}

	filtered := filterExcluded(accounts, []string{"acc-2"})
	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-1", filtered[0].ID)
	assert.Equal(t, "acc-3", filtered[1].ID)
}

func TestMin(t *testing.T) {
	assert.Equal(t, 1, min(1, 2))
	assert.Equal(t, 2, min(3, 2))
	assert.Equal(t, 5, min(5, 5))
}

func TestRouter_GetCurrentAccount(t *testing.T) {
	s := store.NewMemoryStore()
	r := NewRouter(s, DefaultConfig())

	t.Run("no current account", func(t *testing.T) {
		assert.Equal(t, "", r.GetCurrentAccount())
	})

	t.Run("after switch", func(t *testing.T) {
		r.RecordSwitch("acc-1")
		assert.Equal(t, "acc-1", r.GetCurrentAccount())
	})
}

func TestRouter_shouldSwitch(t *testing.T) {
	s := store.NewMemoryStore()

	// Setup accounts
	acc1 := &models.Account{ID: "acc-1", Provider: models.ProviderOpenAI, Enabled: true}
	acc2 := &models.Account{ID: "acc-2", Provider: models.ProviderOpenAI, Enabled: true}
	s.SetAccount(acc1)
	s.SetAccount(acc2)

	// Setup quota
	quota1 := &models.QuotaInfo{
		AccountID:             "acc-1",
		EffectiveRemainingPct: 80.0,
		Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 200, Remaining: 800}},
	}
	quota2 := &models.QuotaInfo{
		AccountID:             "acc-2",
		EffectiveRemainingPct: 50.0,
		Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 500, Remaining: 500}},
	}
	s.SetQuota("acc-1", quota1)
	s.SetQuota("acc-2", quota2)

	r := NewRouter(s, DefaultConfig())
	routerImpl := r.(*router)

	t.Run("switch when current account is critical and new account has good score", func(t *testing.T) {
		// Make acc-1 critical
		quotaCritical := &models.QuotaInfo{
			AccountID:             "acc-1",
			EffectiveRemainingPct: 3.0,
			Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 100, Used: 97, Remaining: 3}},
		}
		s.SetQuota("acc-1", quotaCritical)
		routerImpl.RecordSwitch("acc-1") // Set acc-1 as current

		assert.True(t, routerImpl.shouldSwitch("acc-1", "acc-2", 0.8, 0.1))
	})

	t.Run("don't switch if scores are close (hysteresis)", func(t *testing.T) {
		// Reset quota to non-critical
		s.SetQuota("acc-1", quota1)

		cfg := DefaultConfig()
		cfg.HysteresisMargin = 15.0
		r2 := NewRouter(s, cfg)
		routerImpl2 := r2.(*router)

		routerImpl2.RecordSwitch("acc-1")

		// acc-1 has 80%, acc-2 has 50%
		// Score difference should be significant to switch
		assert.False(t, routerImpl2.shouldSwitch("acc-1", "acc-2", 0.9, 0.88)) // small score gain
	})

	t.Run("don't switch if new account has low score", func(t *testing.T) {
		// Reset quota to non-critical
		s.SetQuota("acc-1", quota1)
		routerImpl.RecordSwitch("acc-1")

		// acc-2 has 50% remaining, which gives lower score
		assert.False(t, routerImpl.shouldSwitch("acc-1", "acc-2", 0.6, 0.8))
	})

	t.Run("switch when new account has much better score", func(t *testing.T) {
		// Reset quota to non-critical
		s.SetQuota("acc-1", quota1)
		routerImpl.RecordSwitch("acc-1")

		// Create scenario where acc-2 has significantly better score
		quotaAcc2High := &models.QuotaInfo{
			AccountID:             "acc-2",
			EffectiveRemainingPct: 95.0,
			Dimensions:            models.DimensionSlice{{Type: models.DimensionRPM, Limit: 1000, Used: 50, Remaining: 950}},
		}
		s.SetQuota("acc-2", quotaAcc2High)

		// Reset dwell time for the switch test
		routerImpl.mu.Lock()
		routerImpl.accountDwellTime = time.Now().Add(-10 * time.Minute)
		routerImpl.mu.Unlock()

		assert.True(t, routerImpl.shouldSwitch("acc-1", "acc-2", 0.98, 0.5))
	})
}
