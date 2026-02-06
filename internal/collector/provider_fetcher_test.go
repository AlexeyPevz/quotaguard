package collector

import (
	"net/http"
	"testing"

	"github.com/quotaguard/quotaguard/internal/models"
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
