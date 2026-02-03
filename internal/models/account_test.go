package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccount_Validate(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid account",
			account: Account{
				ID:               "acc-1",
				Provider:         ProviderOpenAI,
				Tier:             "tier-1",
				Enabled:          true,
				Priority:         1,
				ConcurrencyLimit: 10,
				InputCost:        0.01,
				OutputCost:       0.03,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			account: Account{
				Provider:         ProviderOpenAI,
				ConcurrencyLimit: 10,
			},
			wantErr: true,
			errMsg:  "account ID is required",
		},
		{
			name: "missing provider",
			account: Account{
				ID:               "acc-1",
				ConcurrencyLimit: 10,
			},
			wantErr: true,
			errMsg:  "provider is required",
		},
		{
			name: "negative concurrency limit",
			account: Account{
				ID:               "acc-1",
				Provider:         ProviderOpenAI,
				ConcurrencyLimit: -1,
			},
			wantErr: true,
			errMsg:  "concurrency limit cannot be negative",
		},
		{
			name: "negative input cost",
			account: Account{
				ID:               "acc-1",
				Provider:         ProviderOpenAI,
				ConcurrencyLimit: 10,
				InputCost:        -0.01,
			},
			wantErr: true,
			errMsg:  "input cost cannot be negative",
		},
		{
			name: "negative output cost",
			account: Account{
				ID:               "acc-1",
				Provider:         ProviderOpenAI,
				ConcurrencyLimit: 10,
				OutputCost:       -0.03,
			},
			wantErr: true,
			errMsg:  "output cost cannot be negative",
		},
		{
			name: "zero costs allowed",
			account: Account{
				ID:               "acc-1",
				Provider:         ProviderOpenAI,
				ConcurrencyLimit: 10,
				InputCost:        0,
				OutputCost:       0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.account.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAccount_IsAvailable(t *testing.T) {
	tests := []struct {
		name     string
		account  Account
		expected bool
	}{
		{
			name:     "enabled account",
			account:  Account{ID: "acc-1", Enabled: true},
			expected: true,
		},
		{
			name:     "disabled account",
			account:  Account{ID: "acc-1", Enabled: false},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.account.IsAvailable()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestAccount_EstimatedCost(t *testing.T) {
	account := Account{
		ID:         "acc-1",
		InputCost:  0.01, // $0.01 per 1K input tokens
		OutputCost: 0.03, // $0.03 per 1K output tokens
	}

	tests := []struct {
		name         string
		inputTokens  int64
		outputTokens int64
		expectedCost float64
		tolerance    float64
	}{
		{
			name:         "1000 tokens each",
			inputTokens:  1000,
			outputTokens: 1000,
			expectedCost: 0.04, // 0.01 + 0.03
			tolerance:    0.0001,
		},
		{
			name:         "500 tokens each",
			inputTokens:  500,
			outputTokens: 500,
			expectedCost: 0.02, // 0.005 + 0.015
			tolerance:    0.0001,
		},
		{
			name:         "zero tokens",
			inputTokens:  0,
			outputTokens: 0,
			expectedCost: 0,
			tolerance:    0.0001,
		},
		{
			name:         "only input",
			inputTokens:  2000,
			outputTokens: 0,
			expectedCost: 0.02,
			tolerance:    0.0001,
		},
		{
			name:         "only output",
			inputTokens:  0,
			outputTokens: 3000,
			expectedCost: 0.09,
			tolerance:    0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := account.EstimatedCost(tt.inputTokens, tt.outputTokens)
			assert.InDelta(t, tt.expectedCost, got, tt.tolerance)
		})
	}
}

func TestAccountSlice_FindByID(t *testing.T) {
	accounts := AccountSlice{
		{ID: "acc-1", Provider: ProviderOpenAI},
		{ID: "acc-2", Provider: ProviderAnthropic},
		{ID: "acc-3", Provider: ProviderGemini},
	}

	tests := []struct {
		name         string
		id           string
		wantFound    bool
		wantProvider Provider
	}{
		{"find acc-1", "acc-1", true, ProviderOpenAI},
		{"find acc-2", "acc-2", true, ProviderAnthropic},
		{"find unknown", "acc-999", false, ""},
		{"empty id", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc, found := accounts.FindByID(tt.id)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantProvider, acc.Provider)
			}
		})
	}
}

func TestAccountSlice_FilterEnabled(t *testing.T) {
	accounts := AccountSlice{
		{ID: "acc-1", Enabled: true},
		{ID: "acc-2", Enabled: false},
		{ID: "acc-3", Enabled: true},
		{ID: "acc-4", Enabled: false},
	}

	filtered := accounts.FilterEnabled()

	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-1", filtered[0].ID)
	assert.Equal(t, "acc-3", filtered[1].ID)
}

func TestAccountSlice_FilterByProvider(t *testing.T) {
	accounts := AccountSlice{
		{ID: "acc-1", Provider: ProviderOpenAI},
		{ID: "acc-2", Provider: ProviderAnthropic},
		{ID: "acc-3", Provider: ProviderOpenAI},
		{ID: "acc-4", Provider: ProviderGemini},
	}

	filtered := accounts.FilterByProvider(ProviderOpenAI)

	assert.Len(t, filtered, 2)
	assert.Equal(t, "acc-1", filtered[0].ID)
	assert.Equal(t, "acc-3", filtered[1].ID)
}

func TestAccountSlice_SortByPriority(t *testing.T) {
	accounts := AccountSlice{
		{ID: "acc-1", Priority: 5},
		{ID: "acc-2", Priority: 10},
		{ID: "acc-3", Priority: 1},
		{ID: "acc-4", Priority: 10}, // Same priority
	}

	sorted := accounts.SortByPriority()

	// Should be sorted by priority descending
	assert.Equal(t, 10, sorted[0].Priority)
	assert.Equal(t, 10, sorted[1].Priority)
	assert.Equal(t, 5, sorted[2].Priority)
	assert.Equal(t, 1, sorted[3].Priority)
}

func TestAccount_JSON(t *testing.T) {
	createdAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)

	account := Account{
		ID:               "acc-1",
		Provider:         ProviderOpenAI,
		Tier:             "tier-1",
		Enabled:          true,
		Priority:         5,
		ConcurrencyLimit: 10,
		InputCost:        0.01,
		OutputCost:       0.03,
		CredentialsRef:   "env:OPENAI_API_KEY",
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}

	// Test marshal/unmarshal
	data, err := json.Marshal(account)
	require.NoError(t, err)

	var decoded Account
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, account.ID, decoded.ID)
	assert.Equal(t, account.Provider, decoded.Provider)
	assert.Equal(t, account.Tier, decoded.Tier)
	assert.Equal(t, account.Enabled, decoded.Enabled)
	assert.Equal(t, account.Priority, decoded.Priority)
	assert.Equal(t, account.ConcurrencyLimit, decoded.ConcurrencyLimit)
	assert.Equal(t, account.InputCost, decoded.InputCost)
	assert.Equal(t, account.OutputCost, decoded.OutputCost)
	assert.Equal(t, account.CredentialsRef, decoded.CredentialsRef)
	assert.True(t, account.CreatedAt.Equal(decoded.CreatedAt))
	assert.True(t, account.UpdatedAt.Equal(decoded.UpdatedAt))
}
