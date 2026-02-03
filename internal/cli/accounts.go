package cli

import (
	"fmt"

	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

func seedAccountsFromConfig(s store.Store, cfg *config.Config) error {
	if s == nil || cfg == nil {
		return nil
	}

	for _, acc := range cfg.Accounts {
		account := &models.Account{
			ID:               acc.ID,
			Provider:         models.Provider(acc.Provider),
			Tier:             acc.Tier,
			Enabled:          acc.Enabled,
			Priority:         acc.Priority,
			ConcurrencyLimit: acc.ConcurrencyLimit,
			InputCost:        acc.InputCost,
			OutputCost:       acc.OutputCost,
			CredentialsRef:   acc.CredentialsRef,
		}
		if err := account.Validate(); err != nil {
			return fmt.Errorf("invalid account %s: %w", acc.ID, err)
		}
		s.SetAccount(account)
	}

	return nil
}
