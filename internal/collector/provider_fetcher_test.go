package collector

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/require"
)

func TestAntigravityQuotaFromGroups(t *testing.T) {
	payload := map[string]interface{}{
		"cascadeConfigList": []interface{}{
			map[string]interface{}{
				"clientModelConfigs": []interface{}{
					map[string]interface{}{
						"label": "Gemini 3 Pro High",
						"modelOrAlias": map[string]interface{}{
							"model": "gemini-3-pro-high",
						},
						"quotaInfo": map[string]interface{}{
							"remainingFraction": 0.9,
						},
					},
					map[string]interface{}{
						"label": "Gemini 3 Flash",
						"modelOrAlias": map[string]interface{}{
							"model": "gemini-3-flash-preview",
						},
						"quotaInfo": map[string]interface{}{
							"remainingFraction": 0.5,
						},
					},
					map[string]interface{}{
						"label": "Claude Sonnet 4.5 Thinking",
						"modelOrAlias": map[string]interface{}{
							"model": "gemini-claude-sonnet-4-5-thinking",
						},
						"quotaInfo": map[string]interface{}{
							"remainingFraction": 0.2,
						},
					},
				},
			},
		},
	}

	acc := &models.Account{
		ID:       "antigravity_test",
		Provider: models.ProviderAnthropic,
	}

	quota := antigravityQuotaFromGroups(acc, payload)
	require.NotNil(t, quota)
	require.Len(t, quota.Dimensions, 3)
}

func TestReadFloatOKPercentString(t *testing.T) {
	value, ok := readFloatOK("60%")
	require.True(t, ok)
	require.Equal(t, 60.0, value)
}

func TestShouldRetryAntigravityWithoutProjectID(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid JSON payload received. Unknown name \"projectId\": Cannot find field."}}`)
	require.True(t, shouldRetryAntigravityWithoutProjectID(http.StatusBadRequest, body))
	require.False(t, shouldRetryAntigravityWithoutProjectID(http.StatusUnauthorized, body))
	require.False(t, shouldRetryAntigravityWithoutProjectID(http.StatusBadRequest, []byte(`{"error":"other"}`)))
}

func TestFetchQuota_ClaudeEstimated(t *testing.T) {
	s := store.NewMemoryStore()
	acc := &models.Account{
		ID:       "claude_user_at_example_com",
		Provider: models.ProviderAnthropic,
		Enabled:  true,
	}
	s.SetAccount(acc)
	err := s.SetAccountCredentials(acc.ID, &models.AccountCredentials{
		Type:        "claude",
		AccessToken: "test-token",
		UpdatedAt:   time.Now(),
	})
	require.NoError(t, err)

	pf := NewProviderFetcher(s)
	quota, err := pf.FetchQuota(context.Background(), acc.ID)
	require.NoError(t, err)
	require.NotNil(t, quota)
	require.Equal(t, models.SourceEstimated, quota.Source)
	require.Equal(t, models.DimensionSubscription, quota.Dimensions[0].Type)
}
