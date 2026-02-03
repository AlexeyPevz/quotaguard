package models

import (
	"fmt"
	"sort"
	"time"
)

// Provider represents an LLM provider.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderGemini    Provider = "gemini"
	ProviderAzure     Provider = "azure"
	ProviderOther     Provider = "other"
)

// Account represents an LLM provider account.
type Account struct {
	ID               string    `json:"id"`
	Provider         Provider  `json:"provider"`
	Tier             string    `json:"tier"`
	Enabled          bool      `json:"enabled"`
	Priority         int       `json:"priority"`
	ConcurrencyLimit int       `json:"concurrency_limit"`
	InputCost        float64   `json:"input_cost_per_1k"`  // per 1K tokens
	OutputCost       float64   `json:"output_cost_per_1k"` // per 1K tokens
	CredentialsRef   string    `json:"credentials_ref"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Validate checks if the account is valid.
func (a *Account) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("account ID is required")
	}
	if a.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if a.ConcurrencyLimit < 0 {
		return fmt.Errorf("concurrency limit cannot be negative")
	}
	if a.InputCost < 0 {
		return fmt.Errorf("input cost cannot be negative")
	}
	if a.OutputCost < 0 {
		return fmt.Errorf("output cost cannot be negative")
	}
	return nil
}

// IsAvailable returns true if the account can be used for routing.
func (a *Account) IsAvailable() bool {
	return a.Enabled
}

// EstimatedCost calculates estimated cost for given tokens.
func (a *Account) EstimatedCost(inputTokens, outputTokens int64) float64 {
	inputCost := float64(inputTokens) / 1000.0 * a.InputCost
	outputCost := float64(outputTokens) / 1000.0 * a.OutputCost
	return inputCost + outputCost
}

// AccountSlice is a slice of accounts with helper methods.
type AccountSlice []Account

// FindByID returns an account by ID.
func (as AccountSlice) FindByID(id string) (*Account, bool) {
	for i := range as {
		if as[i].ID == id {
			return &as[i], true
		}
	}
	return nil, false
}

// FilterEnabled returns only enabled accounts.
func (as AccountSlice) FilterEnabled() AccountSlice {
	var result AccountSlice
	for _, a := range as {
		if a.Enabled {
			result = append(result, a)
		}
	}
	return result
}

// FilterByProvider returns accounts for a specific provider.
func (as AccountSlice) FilterByProvider(p Provider) AccountSlice {
	var result AccountSlice
	for _, a := range as {
		if a.Provider == p {
			result = append(result, a)
		}
	}
	return result
}

// SortByPriority sorts accounts by priority (higher first).
func (as AccountSlice) SortByPriority() AccountSlice {
	result := make(AccountSlice, len(as))
	copy(result, as)

	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority > result[j].Priority
	})

	return result
}
