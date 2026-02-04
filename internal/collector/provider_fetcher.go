package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// ProviderFetcher implements QuotaFetcher for multiple providers.
type ProviderFetcher struct {
	store  store.Store
	client *RotatingClient
}

// NewProviderFetcher creates a new provider-aware fetcher.
func NewProviderFetcher(s store.Store) *ProviderFetcher {
	return &ProviderFetcher{
		store:  s,
		client: NewRotatingClient(),
	}
}

// FetchQuota fetches quota for a given account ID.
func (pf *ProviderFetcher) FetchQuota(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
	acc, ok := pf.store.GetAccount(accountID)
	if !ok {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	creds, ok := pf.store.GetAccountCredentials(accountID)
	if !ok {
		return nil, fmt.Errorf("missing credentials for account: %s", accountID)
	}

	switch strings.ToLower(creds.Type) {
	case "codex", "openai":
		return pf.fetchOpenAI(ctx, acc, creds)
	case "antigravity", "cloudcode":
		return pf.fetchAntigravity(ctx, acc, creds)
	case "gemini":
		return pf.fetchGemini(ctx, acc, creds)
	case "qwen", "dashscope":
		return pf.fetchQwen(ctx, acc, creds)
	default:
		// fallback by provider
		switch acc.Provider {
		case models.ProviderOpenAI:
			return pf.fetchOpenAI(ctx, acc, creds)
		case models.ProviderAnthropic:
			return pf.fetchAntigravity(ctx, acc, creds)
		case models.ProviderGemini:
			return pf.fetchGemini(ctx, acc, creds)
		default:
			return nil, fmt.Errorf("unsupported auth type: %s", creds.Type)
		}
	}
}

// ---------------- OpenAI (Codex/ChatGPT) ----------------

type codexSessionResponse struct {
	AccessToken string `json:"accessToken"`
	User        struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func (pf *ProviderFetcher) fetchOpenAI(ctx context.Context, acc *models.Account, creds *models.AccountCredentials) (*models.QuotaInfo, error) {
	sessionToken := strings.TrimSpace(creds.SessionToken)
	if sessionToken == "" {
		return nil, fmt.Errorf("missing session_token")
	}

	jwt, accountID, err := pf.codexJWT(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	usage, headers, err := pf.codexUsage(ctx, jwt, accountID)
	if err != nil {
		return nil, err
	}

	limit, used := parseCodexUsage(usage)
	if limit <= 0 {
		if headerLimit, headerRemaining, headerReset := parseRateLimitHeaders(headers); headerRemaining >= 0 {
			limit = headerLimit
			used = maxInt(0, headerLimit-headerRemaining)
			if headerLimit == 0 {
				limit = headerRemaining
			}
			return quotaFromNumbers(acc, limit, used, headerReset, 0.6), nil
		}
		return nil, fmt.Errorf("codex usage response missing limits")
	}

	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}

	return quotaFromNumbers(acc, limit, used, nil, 0.7), nil
}

func (pf *ProviderFetcher) codexJWT(ctx context.Context, sessionToken string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/api/auth/session", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Cookie", "__Secure-next-auth.session-token="+sessionToken)

	resp, err := pf.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("codex session status %d", resp.StatusCode)
	}

	var parsed codexSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", err
	}
	if parsed.AccessToken == "" || parsed.User.ID == "" {
		return "", "", fmt.Errorf("codex session response missing token")
	}
	return parsed.AccessToken, parsed.User.ID, nil
}

func (pf *ProviderFetcher) codexUsage(ctx context.Context, jwtToken, accountID string) (map[string]interface{}, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("ChatGPT-Account-Id", accountID)
	req.Header.Set("OAI-Language", "en-US")

	resp, err := pf.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.Header, rateLimitErrorFromHeaders(resp.Header, "codex rate limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.Header, fmt.Errorf("codex usage status %d", resp.StatusCode)
	}

	var parsed map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.Header, err
	}
	return parsed, resp.Header, nil
}

func parseCodexUsage(payload map[string]interface{}) (limit int, used int) {
	if payload == nil {
		return 0, 0
	}
	// Try usage.product_surface_usage_values
	if usage, ok := payload["usage"]; ok {
		limit, used = parseProductSurfaceUsage(usage)
		if limit > 0 {
			return limit, used
		}
	}
	limit, used = parseProductSurfaceUsage(payload)
	return limit, used
}

func parseProductSurfaceUsage(value interface{}) (limit int, used int) {
	var usedSum float64
	var limitSum float64
	var found bool

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			for k, v2 := range t {
				low := strings.ToLower(k)
				if low == "product_surface_usage_values" {
					if arr, ok := v2.([]interface{}); ok {
						for _, item := range arr {
							switch n := item.(type) {
							case float64:
								usedSum += n
								found = true
							case map[string]interface{}:
								if val, ok := n["value"]; ok {
									usedSum += readFloat(val)
									found = true
								}
								if val, ok := n["used"]; ok {
									usedSum += readFloat(val)
									found = true
								}
								if val, ok := n["limit"]; ok {
									limitSum += readFloat(val)
									found = true
								}
							}
						}
					}
				}
				if low == "product_surface_usage_limit_values" || low == "product_surface_usage_limits" {
					if arr, ok := v2.([]interface{}); ok {
						for _, item := range arr {
							limitSum += readFloat(item)
						}
					}
				}
				walk(v2)
			}
		case []interface{}:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(value)

	if !found {
		return 0, 0
	}
	used = int(usedSum)
	limit = int(limitSum)
	return limit, used
}

// ---------------- Antigravity (Google Cloud Code) ----------------

func (pf *ProviderFetcher) fetchAntigravity(ctx context.Context, acc *models.Account, creds *models.AccountCredentials) (*models.QuotaInfo, error) {
	refreshToken := strings.TrimSpace(creds.RefreshToken)
	if refreshToken == "" {
		return nil, fmt.Errorf("missing refresh_token")
	}

	clientID := strings.TrimSpace(creds.ClientID)
	clientSecret := strings.TrimSpace(creds.ClientSecret)
	if clientID == "" {
		clientID = strings.TrimSpace(os.Getenv("QUOTAGUARD_GOOGLE_CLIENT_ID"))
	}
	if clientSecret == "" {
		clientSecret = strings.TrimSpace(os.Getenv("QUOTAGUARD_GOOGLE_CLIENT_SECRET"))
	}
	if clientID == "" {
		return nil, fmt.Errorf("missing Google OAuth client_id")
	}

	accessToken, err := pf.refreshGoogleAccessToken(ctx, clientID, clientSecret, refreshToken)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "antigravity/1.104.0 darwin/arm64")

	resp, err := pf.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, rateLimitErrorFromHeaders(resp.Header, "antigravity rate limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("antigravity status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	remainingFraction, resetAt, ok := parseQuotaFraction(payload)
	if !ok {
		return nil, fmt.Errorf("antigravity quota not found")
	}
	remainingPct := remainingFraction * 100
	if remainingPct < 0 {
		remainingPct = 0
	}
	if remainingPct > 100 {
		remainingPct = 100
	}

	limit := int64(100)
	remaining := int64(remainingPct)
	used := limit - remaining
	return quotaFromNumbers(acc, int(limit), int(used), resetAt, 0.8), nil
}

func (pf *ProviderFetcher) refreshGoogleAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := pf.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", rateLimitErrorFromHeaders(resp.Header, "google oauth rate limit")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oauth status %d", resp.StatusCode)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.AccessToken == "" {
		return "", errors.New("oauth response missing access_token")
	}
	return parsed.AccessToken, nil
}

// ---------------- Gemini (countTokens) ----------------

func (pf *ProviderFetcher) fetchGemini(ctx context.Context, acc *models.Account, creds *models.AccountCredentials) (*models.QuotaInfo, error) {
	apiKey := strings.TrimSpace(creds.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("missing api_key")
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:countTokens?key=%s", apiKey)
	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": "."},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := pf.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, rateLimitErrorFromHeaders(resp.Header, "gemini rate limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini status %d", resp.StatusCode)
	}

	limit, remaining, resetAt := parseRateLimitHeaders(resp.Header)
	if remaining < 0 {
		return nil, fmt.Errorf("gemini rate limit headers not found")
	}
	if limit == 0 {
		limit = remaining
	}
	used := limit - remaining
	if used < 0 {
		used = 0
	}

	return quotaFromNumbers(acc, limit, used, resetAt, 0.5), nil
}

// ---------------- Qwen (DashScope) ----------------

func (pf *ProviderFetcher) fetchQwen(ctx context.Context, acc *models.Account, creds *models.AccountCredentials) (*models.QuotaInfo, error) {
	apiKey := strings.TrimSpace(creds.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("missing api_key")
	}

	payload := map[string]interface{}{
		"model": "qwen-turbo",
		"input": map[string]interface{}{
			"prompt": "ping",
		},
		"parameters": map[string]interface{}{
			"result_format": "message",
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://dashscope.aliyuncs.com/api/v1/services/aigc/text-generation/generation", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := pf.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, rateLimitErrorFromHeaders(resp.Header, "qwen rate limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qwen status %d", resp.StatusCode)
	}

	limit, remaining, resetAt := parseRateLimitHeaders(resp.Header)
	if remaining < 0 {
		return nil, fmt.Errorf("qwen rate limit headers not found")
	}
	if limit == 0 {
		limit = remaining
	}
	used := limit - remaining
	if used < 0 {
		used = 0
	}
	return quotaFromNumbers(acc, limit, used, resetAt, 0.4), nil
}

// ---------------- Helpers ----------------

func quotaFromNumbers(acc *models.Account, limit int, used int, resetAt *time.Time, confidence float64) *models.QuotaInfo {
	if limit < 0 {
		limit = 0
	}
	if used < 0 {
		used = 0
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	dim := models.Dimension{
		Type:       models.DimensionSubscription,
		Limit:      int64(limit),
		Used:       int64(used),
		Remaining:  int64(remaining),
		ResetAt:    resetAt,
		Semantics:  models.WindowFixed,
		Source:     models.SourcePolling,
		Confidence: confidence,
	}

	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Dimensions = models.DimensionSlice{dim}
	quota.Source = models.SourcePolling
	quota.Confidence = confidence
	quota.CollectedAt = time.Now()
	quota.UpdateEffective()
	return quota
}

func readFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

func parseQuotaFraction(payload map[string]interface{}) (float64, *time.Time, bool) {
	var fraction float64
	var resetAt *time.Time
	var found bool

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if q, ok := t["quota"].(map[string]interface{}); ok {
				if rf, ok := q["remainingFraction"]; ok {
					fraction = readFloat(rf)
					found = true
				}
				if rt, ok := q["resetTime"].(string); ok {
					if parsed, err := time.Parse(time.RFC3339, rt); err == nil {
						resetAt = &parsed
					}
				}
			}
			if rf, ok := t["remainingFraction"]; ok && !found {
				fraction = readFloat(rf)
				found = true
			}
			if rt, ok := t["resetTime"].(string); ok && resetAt == nil {
				if parsed, err := time.Parse(time.RFC3339, rt); err == nil {
					resetAt = &parsed
				}
			}
			for _, v2 := range t {
				walk(v2)
			}
		case []interface{}:
			for _, item := range t {
				walk(item)
			}
		}
	}
	walk(payload)

	return fraction, resetAt, found
}

func parseRateLimitHeaders(headers http.Header) (limit int, remaining int, resetAt *time.Time) {
	remaining = -1
	limitVal := headerFirst(headers, "x-ratelimit-limit-requests", "x-goog-ratelimit-limit-requests")
	if limitVal != "" {
		limit = parseInt(limitVal)
	}
	remainingVal := headerFirst(headers, "x-ratelimit-remaining-requests", "x-goog-ratelimit-remaining-requests")
	if remainingVal != "" {
		remaining = parseInt(remainingVal)
	}
	resetVal := headerFirst(headers, "x-ratelimit-reset-requests", "x-goog-ratelimit-reset-requests", "retry-after")
	if resetVal != "" {
		if secs, err := strconv.ParseInt(resetVal, 10, 64); err == nil {
			t := time.Now().Add(time.Duration(secs) * time.Second)
			resetAt = &t
		} else if parsed, err := http.ParseTime(resetVal); err == nil {
			resetAt = &parsed
		}
	}
	return limit, remaining, resetAt
}

func rateLimitErrorFromHeaders(headers http.Header, msg string) *RateLimitError {
	_, _, resetAt := parseRateLimitHeaders(headers)
	if resetAt == nil {
		return &RateLimitError{RetryAfter: 30 * time.Second, Message: msg}
	}
	retryAfter := time.Until(*resetAt)
	if retryAfter < 0 {
		retryAfter = 30 * time.Second
	}
	return &RateLimitError{RetryAfter: retryAfter, Message: msg}
}

func headerFirst(headers http.Header, keys ...string) string {
	for _, k := range keys {
		if v := headers.Get(k); v != "" {
			return v
		}
	}
	return ""
}

func parseInt(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	n, _ := strconv.Atoi(value)
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ QuotaFetcher = (*ProviderFetcher)(nil)
