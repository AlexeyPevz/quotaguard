package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
)

func main() {
	tmpDir, err := os.MkdirTemp("", "router-debug")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "router.db")
	st, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		panic(err)
	}
	defer st.Close()

	cfg := router.DefaultConfig()
	r := router.NewRouter(st, cfg)

	accounts := []struct {
		id       string
		priority int
		quota    float64
	}{
		{"multi-1", 5, 50.0},
		{"multi-2", 5, 70.0},
		{"multi-3", 5, 90.0},
		{"multi-4", 5, 30.0},
	}

	for _, acc := range accounts {
		st.SetAccount(&models.Account{ID: acc.id, Provider: models.ProviderOpenAI, Enabled: true, Priority: acc.priority, InputCost: 0.01, OutputCost: 0.03})
		st.SetQuota(acc.id, &models.QuotaInfo{AccountID: acc.id, Provider: models.ProviderOpenAI, EffectiveRemainingPct: acc.quota, Confidence: 0.95})
	}

	// simulate prior switch to multi-1
	r.RecordSwitch("multi-1")

	resp, err := r.Select(context.Background(), router.SelectRequest{Provider: models.ProviderOpenAI, Policy: "balanced"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("selected after record switch: %s score=%.4f reason=%s\n", resp.AccountID, resp.Score, resp.Reason)
}
