// Package headers provides parsing of LLM provider response headers for quota information.
package headers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
)

// Parser defines the interface for parsing provider-specific headers
type Parser interface {
	// Parse extracts quota information from HTTP response headers
	Parse(headers http.Header, accountID string) (*models.QuotaInfo, error)
	// Provider returns the provider name this parser handles
	Provider() models.Provider
}

// OpenAIParser parses OpenAI response headers
type OpenAIParser struct{}

// Provider returns the provider name
func (p *OpenAIParser) Provider() models.Provider {
	return models.ProviderOpenAI
}

// Parse extracts quota information from OpenAI response headers
func (p *OpenAIParser) Parse(headers http.Header, accountID string) (*models.QuotaInfo, error) {
	quota := &models.QuotaInfo{
		AccountID:   accountID,
		Provider:    models.ProviderOpenAI,
		Source:      models.SourceHeaders,
		CollectedAt: time.Now(),
		Confidence:  1.0,
	}

	// Parse rate limit headers
	// x-ratelimit-limit-requests: 10000
	// x-ratelimit-limit-tokens: 2000000
	// x-ratelimit-remaining-requests: 9999
	// x-ratelimit-remaining-tokens: 1999999
	// x-ratelimit-reset-requests: 0s
	// x-ratelimit-reset-tokens: 0s

	reqLimit := parseIntHeader(headers, "X-Ratelimit-Limit-Requests")
	reqRemaining := parseIntHeader(headers, "X-Ratelimit-Remaining-Requests")
	tokenLimit := parseIntHeader(headers, "X-Ratelimit-Limit-Tokens")
	tokenRemaining := parseIntHeader(headers, "X-Ratelimit-Remaining-Tokens")

	// Build dimensions
	var dimensions models.DimensionSlice

	if reqLimit > 0 {
		used := reqLimit - reqRemaining
		dimensions = append(dimensions, models.Dimension{
			Type:      models.DimensionRPM,
			Limit:     reqLimit,
			Used:      used,
			Remaining: reqRemaining,
		})
	}

	if tokenLimit > 0 {
		used := tokenLimit - tokenRemaining
		dimensions = append(dimensions, models.Dimension{
			Type:      models.DimensionTPM,
			Limit:     tokenLimit,
			Used:      used,
			Remaining: tokenRemaining,
		})
	}

	quota.Dimensions = dimensions
	quota.UpdateEffective()

	if len(dimensions) == 0 {
		return nil, fmt.Errorf("no quota headers found")
	}

	return quota, nil
}

// AnthropicParser parses Anthropic response headers
type AnthropicParser struct{}

// Provider returns the provider name
func (p *AnthropicParser) Provider() models.Provider {
	return models.ProviderAnthropic
}

// Parse extracts quota information from Anthropic response headers
func (p *AnthropicParser) Parse(headers http.Header, accountID string) (*models.QuotaInfo, error) {
	quota := &models.QuotaInfo{
		AccountID:   accountID,
		Provider:    models.ProviderAnthropic,
		Source:      models.SourceHeaders,
		CollectedAt: time.Now(),
		Confidence:  1.0,
	}

	// Anthropic uses different header format:
	// anthropic-ratelimit-requests-limit: 1000
	// anthropic-ratelimit-requests-remaining: 999
	// anthropic-ratelimit-tokens-limit: 100000
	// anthropic-ratelimit-tokens-remaining: 99999

	reqLimit := parseIntHeader(headers, "Anthropic-Ratelimit-Requests-Limit")
	reqRemaining := parseIntHeader(headers, "Anthropic-Ratelimit-Requests-Remaining")
	tokenLimit := parseIntHeader(headers, "Anthropic-Ratelimit-Tokens-Limit")
	tokenRemaining := parseIntHeader(headers, "Anthropic-Ratelimit-Tokens-Remaining")

	var dimensions models.DimensionSlice

	if reqLimit > 0 {
		used := reqLimit - reqRemaining
		dimensions = append(dimensions, models.Dimension{
			Type:      models.DimensionRPM,
			Limit:     reqLimit,
			Used:      used,
			Remaining: reqRemaining,
		})
	}

	if tokenLimit > 0 {
		used := tokenLimit - tokenRemaining
		dimensions = append(dimensions, models.Dimension{
			Type:      models.DimensionTPM,
			Limit:     tokenLimit,
			Used:      used,
			Remaining: tokenRemaining,
		})
	}

	quota.Dimensions = dimensions
	quota.UpdateEffective()

	if len(dimensions) == 0 {
		return nil, fmt.Errorf("no quota headers found")
	}

	return quota, nil
}

// GeminiParser parses Google Gemini response headers
type GeminiParser struct{}

// Provider returns the provider name
func (p *GeminiParser) Provider() models.Provider {
	return models.ProviderGemini
}

// Parse extracts quota information from Gemini response headers
func (p *GeminiParser) Parse(headers http.Header, accountID string) (*models.QuotaInfo, error) {
	quota := &models.QuotaInfo{
		AccountID:   accountID,
		Provider:    models.ProviderGemini,
		Source:      models.SourceHeaders,
		CollectedAt: time.Now(),
		Confidence:  1.0,
	}

	// Gemini uses different header format:
	// x-goog-quota-limit: requestsPerMinute=1000, tokensPerMinute=100000
	// x-goog-quota-remaining: requestsPerMinute=999, tokensPerMinute=99999

	quotaMap := parseQuotaHeader(headers.Get("X-Goog-Quota-Limit"))
	remainingMap := parseQuotaHeader(headers.Get("X-Goog-Quota-Remaining"))

	var dimensions models.DimensionSlice

	if reqLimit, ok := quotaMap["requestsperminute"]; ok {
		reqRemaining := remainingMap["requestsperminute"]
		used := reqLimit - reqRemaining
		dimensions = append(dimensions, models.Dimension{
			Type:      models.DimensionRPM,
			Limit:     reqLimit,
			Used:      used,
			Remaining: reqRemaining,
		})
	}

	if tokenLimit, ok := quotaMap["tokensperminute"]; ok {
		tokenRemaining := remainingMap["tokensperminute"]
		used := tokenLimit - tokenRemaining
		dimensions = append(dimensions, models.Dimension{
			Type:      models.DimensionTPM,
			Limit:     tokenLimit,
			Used:      used,
			Remaining: tokenRemaining,
		})
	}

	quota.Dimensions = dimensions
	quota.UpdateEffective()

	if len(dimensions) == 0 {
		return nil, fmt.Errorf("no quota headers found")
	}

	return quota, nil
}

// Registry manages parsers for different providers
type Registry struct {
	parsers map[models.Provider]Parser
}

// NewRegistry creates a new parser registry with default parsers
func NewRegistry() *Registry {
	r := &Registry{
		parsers: make(map[models.Provider]Parser),
	}

	// Register default parsers
	r.Register(&OpenAIParser{})
	r.Register(&AnthropicParser{})
	r.Register(&GeminiParser{})

	return r
}

// Register adds a parser to the registry
func (r *Registry) Register(parser Parser) {
	r.parsers[parser.Provider()] = parser
}

// Get retrieves a parser for the given provider
func (r *Registry) Get(provider models.Provider) (Parser, bool) {
	parser, ok := r.parsers[provider]
	return parser, ok
}

// Parse attempts to parse headers using the appropriate provider parser
func (r *Registry) Parse(provider models.Provider, headers http.Header, accountID string) (*models.QuotaInfo, error) {
	parser, ok := r.Get(provider)
	if !ok {
		return nil, fmt.Errorf("no parser registered for provider: %s", provider)
	}
	return parser.Parse(headers, accountID)
}

// AutoDetect attempts to detect the provider from headers and parse accordingly
func (r *Registry) AutoDetect(headers http.Header, accountID string) (*models.QuotaInfo, models.Provider, error) {
	// Try to detect provider from headers
	if hasOpenAIHeaders(headers) {
		quota, err := r.Parse(models.ProviderOpenAI, headers, accountID)
		return quota, models.ProviderOpenAI, err
	}

	if hasAnthropicHeaders(headers) {
		quota, err := r.Parse(models.ProviderAnthropic, headers, accountID)
		return quota, models.ProviderAnthropic, err
	}

	if hasGeminiHeaders(headers) {
		quota, err := r.Parse(models.ProviderGemini, headers, accountID)
		return quota, models.ProviderGemini, err
	}

	return nil, "", fmt.Errorf("unable to detect provider from headers")
}

// Helper functions

func parseIntHeader(headers http.Header, key string) int64 {
	val := headers.Get(key)
	if val == "" {
		return 0
	}

	// Handle duration format like "0s", "60s"
	if strings.HasSuffix(val, "s") {
		d, err := time.ParseDuration(val)
		if err != nil {
			return 0
		}
		return int64(d.Seconds())
	}

	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseQuotaHeader(header string) map[string]int64 {
	result := make(map[string]int64)
	if header == "" {
		return result
	}

	// Format: "requestsPerMinute=1000, tokensPerMinute=100000"
	pairs := strings.Split(header, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		kv := strings.Split(pair, "=")
		if len(kv) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}

		result[key] = val
	}

	return result
}

func hasOpenAIHeaders(headers http.Header) bool {
	return headers.Get("X-Ratelimit-Limit-Requests") != "" ||
		headers.Get("X-Ratelimit-Remaining-Requests") != ""
}

func hasAnthropicHeaders(headers http.Header) bool {
	return headers.Get("Anthropic-Ratelimit-Requests-Limit") != "" ||
		headers.Get("Anthropic-Ratelimit-Requests-Remaining") != ""
}

func hasGeminiHeaders(headers http.Header) bool {
	return headers.Get("X-Goog-Quota-Limit") != "" ||
		headers.Get("X-Goog-Quota-Remaining") != ""
}
