package headers

import (
	"net/http"
	"testing"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIParser_Provider(t *testing.T) {
	parser := &OpenAIParser{}
	assert.Equal(t, models.ProviderOpenAI, parser.Provider())
}

func TestOpenAIParser_Parse(t *testing.T) {
	parser := &OpenAIParser{}

	t.Run("valid headers", func(t *testing.T) {
		headers := http.Header{
			"X-Ratelimit-Limit-Requests":     []string{"10000"},
			"X-Ratelimit-Remaining-Requests": []string{"9999"},
			"X-Ratelimit-Limit-Tokens":       []string{"2000000"},
			"X-Ratelimit-Remaining-Tokens":   []string{"1999999"},
		}

		quota, err := parser.Parse(headers, "acc-1")
		require.NoError(t, err)
		assert.Equal(t, "acc-1", quota.AccountID)
		assert.Equal(t, models.ProviderOpenAI, quota.Provider)
		assert.Equal(t, models.SourceHeaders, quota.Source)
		assert.Equal(t, 1.0, quota.Confidence)
		assert.Len(t, quota.Dimensions, 2)

		// Check RPM dimension
		rpm, ok := quota.Dimensions.FindByType(models.DimensionRPM)
		require.True(t, ok)
		assert.Equal(t, int64(10000), rpm.Limit)
		assert.Equal(t, int64(1), rpm.Used)
		assert.Equal(t, int64(9999), rpm.Remaining)

		// Check TPM dimension
		tpm, ok := quota.Dimensions.FindByType(models.DimensionTPM)
		require.True(t, ok)
		assert.Equal(t, int64(2000000), tpm.Limit)
		assert.Equal(t, int64(1), tpm.Used)
	})

	t.Run("only requests", func(t *testing.T) {
		headers := http.Header{
			"X-Ratelimit-Limit-Requests":     []string{"1000"},
			"X-Ratelimit-Remaining-Requests": []string{"500"},
		}

		quota, err := parser.Parse(headers, "acc-1")
		require.NoError(t, err)
		assert.Len(t, quota.Dimensions, 1)
	})

	t.Run("no headers", func(t *testing.T) {
		headers := http.Header{}

		_, err := parser.Parse(headers, "acc-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no quota headers found")
	})

	t.Run("with reset duration", func(t *testing.T) {
		headers := http.Header{
			"X-Ratelimit-Limit-Requests":     []string{"1000"},
			"X-Ratelimit-Remaining-Requests": []string{"500"},
			"X-Ratelimit-Reset-Requests":     []string{"60s"},
		}

		quota, err := parser.Parse(headers, "acc-1")
		require.NoError(t, err)
		assert.NotNil(t, quota)
	})
}

func TestAnthropicParser_Provider(t *testing.T) {
	parser := &AnthropicParser{}
	assert.Equal(t, models.ProviderAnthropic, parser.Provider())
}

func TestAnthropicParser_Parse(t *testing.T) {
	parser := &AnthropicParser{}

	t.Run("valid headers", func(t *testing.T) {
		headers := http.Header{
			"Anthropic-Ratelimit-Requests-Limit":     []string{"1000"},
			"Anthropic-Ratelimit-Requests-Remaining": []string{"999"},
			"Anthropic-Ratelimit-Tokens-Limit":       []string{"100000"},
			"Anthropic-Ratelimit-Tokens-Remaining":   []string{"99999"},
		}

		quota, err := parser.Parse(headers, "acc-1")
		require.NoError(t, err)
		assert.Equal(t, models.ProviderAnthropic, quota.Provider)
		assert.Len(t, quota.Dimensions, 2)

		rpm, ok := quota.Dimensions.FindByType(models.DimensionRPM)
		require.True(t, ok)
		assert.Equal(t, int64(1000), rpm.Limit)
		assert.Equal(t, int64(1), rpm.Used)
	})

	t.Run("no headers", func(t *testing.T) {
		headers := http.Header{}

		_, err := parser.Parse(headers, "acc-1")
		assert.Error(t, err)
	})
}

func TestGeminiParser_Provider(t *testing.T) {
	parser := &GeminiParser{}
	assert.Equal(t, models.ProviderGemini, parser.Provider())
}

func TestGeminiParser_Parse(t *testing.T) {
	parser := &GeminiParser{}

	t.Run("valid headers", func(t *testing.T) {
		headers := http.Header{
			"X-Goog-Quota-Limit":     []string{"requestsPerMinute=1000, tokensPerMinute=100000"},
			"X-Goog-Quota-Remaining": []string{"requestsPerMinute=999, tokensPerMinute=99999"},
		}

		quota, err := parser.Parse(headers, "acc-1")
		require.NoError(t, err)
		assert.Equal(t, models.ProviderGemini, quota.Provider)
		assert.Len(t, quota.Dimensions, 2)

		rpm, ok := quota.Dimensions.FindByType(models.DimensionRPM)
		require.True(t, ok)
		assert.Equal(t, int64(1000), rpm.Limit)
		assert.Equal(t, int64(1), rpm.Used)
	})

	t.Run("only requests", func(t *testing.T) {
		headers := http.Header{
			"X-Goog-Quota-Limit":     []string{"requestsPerMinute=1000"},
			"X-Goog-Quota-Remaining": []string{"requestsPerMinute=500"},
		}

		quota, err := parser.Parse(headers, "acc-1")
		require.NoError(t, err)
		assert.Len(t, quota.Dimensions, 1)
	})

	t.Run("no headers", func(t *testing.T) {
		headers := http.Header{}

		_, err := parser.Parse(headers, "acc-1")
		assert.Error(t, err)
	})
}

func TestRegistry(t *testing.T) {
	t.Run("new registry with defaults", func(t *testing.T) {
		registry := NewRegistry()
		require.NotNil(t, registry)

		// Check all default parsers are registered
		_, ok := registry.Get(models.ProviderOpenAI)
		assert.True(t, ok)

		_, ok = registry.Get(models.ProviderAnthropic)
		assert.True(t, ok)

		_, ok = registry.Get(models.ProviderGemini)
		assert.True(t, ok)
	})

	t.Run("register and get", func(t *testing.T) {
		registry := NewRegistry()
		customParser := &OpenAIParser{}

		registry.Register(customParser)

		parser, ok := registry.Get(models.ProviderOpenAI)
		assert.True(t, ok)
		assert.NotNil(t, parser)
	})

	t.Run("get non-existent", func(t *testing.T) {
		registry := NewRegistry()

		_, ok := registry.Get("unknown")
		assert.False(t, ok)
	})

	t.Run("parse with registry", func(t *testing.T) {
		registry := NewRegistry()

		headers := http.Header{
			"X-Ratelimit-Limit-Requests":     []string{"1000"},
			"X-Ratelimit-Remaining-Requests": []string{"500"},
		}

		quota, err := registry.Parse(models.ProviderOpenAI, headers, "acc-1")
		require.NoError(t, err)
		assert.NotNil(t, quota)
	})

	t.Run("parse unknown provider", func(t *testing.T) {
		registry := NewRegistry()

		headers := http.Header{}

		_, err := registry.Parse("unknown", headers, "acc-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no parser registered")
	})
}

func TestRegistry_AutoDetect(t *testing.T) {
	registry := NewRegistry()

	t.Run("detect OpenAI", func(t *testing.T) {
		headers := http.Header{
			"X-Ratelimit-Limit-Requests": []string{"1000"},
		}

		quota, provider, err := registry.AutoDetect(headers, "acc-1")
		require.NoError(t, err)
		assert.Equal(t, models.ProviderOpenAI, provider)
		assert.NotNil(t, quota)
	})

	t.Run("detect Anthropic", func(t *testing.T) {
		headers := http.Header{
			"Anthropic-Ratelimit-Requests-Limit": []string{"1000"},
		}

		quota, provider, err := registry.AutoDetect(headers, "acc-1")
		require.NoError(t, err)
		assert.Equal(t, models.ProviderAnthropic, provider)
		assert.NotNil(t, quota)
	})

	t.Run("detect Gemini", func(t *testing.T) {
		headers := http.Header{
			"X-Goog-Quota-Limit": []string{"requestsPerMinute=1000"},
		}

		quota, provider, err := registry.AutoDetect(headers, "acc-1")
		require.NoError(t, err)
		assert.Equal(t, models.ProviderGemini, provider)
		assert.NotNil(t, quota)
	})

	t.Run("unable to detect", func(t *testing.T) {
		headers := http.Header{}

		_, _, err := registry.AutoDetect(headers, "acc-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unable to detect provider")
	})
}

func TestParseIntHeader(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected int64
	}{
		{"valid integer", "1000", 1000},
		{"valid duration", "60s", 60},
		{"empty string", "", 0},
		{"invalid value", "abc", 0},
		{"zero", "0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{"X-Test": []string{tt.value}}
			result := parseIntHeader(headers, "X-Test")
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseQuotaHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected map[string]int64
	}{
		{
			name:     "valid quota header",
			header:   "requestsPerMinute=1000, tokensPerMinute=100000",
			expected: map[string]int64{"requestsperminute": 1000, "tokensperminute": 100000},
		},
		{
			name:     "single value",
			header:   "requestsPerMinute=500",
			expected: map[string]int64{"requestsperminute": 500},
		},
		{
			name:     "empty header",
			header:   "",
			expected: map[string]int64{},
		},
		{
			name:     "invalid format",
			header:   "invalid",
			expected: map[string]int64{},
		},
		{
			name:     "mixed valid and invalid",
			header:   "valid=100, invalid, another=200",
			expected: map[string]int64{"valid": 100, "another": 200},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseQuotaHeader(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasHeaders(t *testing.T) {
	t.Run("hasOpenAIHeaders", func(t *testing.T) {
		assert.True(t, hasOpenAIHeaders(http.Header{
			"X-Ratelimit-Limit-Requests": []string{"1000"},
		}))
		assert.True(t, hasOpenAIHeaders(http.Header{
			"X-Ratelimit-Remaining-Requests": []string{"500"},
		}))
		assert.False(t, hasOpenAIHeaders(http.Header{}))
	})

	t.Run("hasAnthropicHeaders", func(t *testing.T) {
		assert.True(t, hasAnthropicHeaders(http.Header{
			"Anthropic-Ratelimit-Requests-Limit": []string{"1000"},
		}))
		assert.True(t, hasAnthropicHeaders(http.Header{
			"Anthropic-Ratelimit-Requests-Remaining": []string{"500"},
		}))
		assert.False(t, hasAnthropicHeaders(http.Header{}))
	})

	t.Run("hasGeminiHeaders", func(t *testing.T) {
		assert.True(t, hasGeminiHeaders(http.Header{
			"X-Goog-Quota-Limit": []string{"requestsPerMinute=1000"},
		}))
		assert.True(t, hasGeminiHeaders(http.Header{
			"X-Goog-Quota-Remaining": []string{"requestsPerMinute=500"},
		}))
		assert.False(t, hasGeminiHeaders(http.Header{}))
	})
}
