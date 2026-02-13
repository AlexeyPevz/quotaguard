package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	case "claude", "claude-code", "claude_code":
		if strings.TrimSpace(creds.SessionToken) == "" && strings.TrimSpace(creds.AccessToken) == "" {
			return nil, fmt.Errorf("missing claude auth token")
		}
		return claudeEstimatedQuota(acc), nil
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
	jwt := strings.TrimSpace(creds.AccessToken)
	accountID := strings.TrimSpace(creds.ProviderAccountID)

	if sessionToken != "" {
		var err error
		jwt, accountID, err = pf.codexJWT(ctx, sessionToken)
		if err != nil {
			return nil, err
		}
	} else {
		if jwt == "" || accountID == "" {
			return nil, fmt.Errorf("missing session_token or access_token/account_id")
		}
	}

	usage, headers, err := pf.codexUsage(ctx, jwt, accountID)
	if err != nil {
		return nil, err
	}

	if quota, ok := codexQuotaFromRateLimit(acc, usage); ok {
		return quota, nil
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

func codexQuotaFromRateLimit(acc *models.Account, usage map[string]interface{}) (*models.QuotaInfo, bool) {
	rateLimit, ok := usage["rate_limit"].(map[string]interface{})
	if !ok {
		return nil, false
	}

	dims := make(models.DimensionSlice, 0, 3)
	addWindow := func(name string, window any) {
		m, ok := window.(map[string]interface{})
		if !ok {
			return
		}
		if _, ok := m["used_percent"]; !ok {
			return
		}
		usedPct := readFloat(m["used_percent"])
		limit := int64(100)
		used := int64(usedPct + 0.5)
		resetAt := parseUnixTimePtr(m["reset_at"])
		dims = append(dims, models.Dimension{
			Name:       name,
			Type:       models.DimensionType("WINDOW"),
			Limit:      limit,
			Used:       used,
			Remaining:  maxInt64(0, limit-used),
			ResetAt:    resetAt,
			Semantics:  models.WindowFixed,
			Source:     models.SourcePolling,
			Confidence: 0.6,
		})
	}

	addWindow("Codex primary", rateLimit["primary_window"])
	addWindow("Codex secondary", rateLimit["secondary_window"])

	if review, ok := usage["code_review_rate_limit"].(map[string]interface{}); ok {
		addWindow("Code review primary", review["primary_window"])
	}

	if len(dims) == 0 {
		return nil, false
	}

	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Source = models.SourcePolling
	quota.Confidence = 0.6
	quota.Dimensions = dims
	quota.UpdateEffective()
	return quota, true
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
		clientID = firstNonEmpty(
			strings.TrimSpace(os.Getenv("QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID")),
			strings.TrimSpace(os.Getenv("QUOTAGUARD_GOOGLE_CLIENT_ID")),
		)
	}
	if clientSecret == "" {
		clientSecret = firstNonEmpty(
			strings.TrimSpace(os.Getenv("QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET")),
			strings.TrimSpace(os.Getenv("QUOTAGUARD_GOOGLE_CLIENT_SECRET")),
		)
	}
	if clientID == "" {
		return nil, fmt.Errorf("missing Google OAuth client_id")
	}
	creds.ClientID = clientID
	creds.ClientSecret = clientSecret
	if creds.TokenURI == "" {
		creds.TokenURI = "https://oauth2.googleapis.com/token"
	}

	secrets := antigravityCandidateSecrets(clientSecret)
	var accessToken string
	var lastErr error
	for _, secret := range secrets {
		creds.ClientSecret = secret
		token, err := pf.ensureOAuthToken(ctx, acc.ID, creds, "https://oauth2.googleapis.com/token")
		if err == nil {
			accessToken = token
			break
		}
		lastErr = err
	}
	if accessToken == "" {
		return nil, fmt.Errorf("antigravity oauth: %w", lastErr)
	}

	projectID := strings.TrimSpace(creds.ProjectID)
	if strings.Contains(projectID, ",") {
		for _, p := range strings.Split(projectID, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				projectID = p
				break
			}
		}
	}

	bodyPayload := map[string]interface{}{}
	if projectID != "" {
		bodyPayload["projectId"] = projectID
	}
	doFetch := func(payload map[string]interface{}, withProjectHeaders bool) (int, []byte, http.Header, error) {
		bodyBytes, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", bytes.NewReader(bodyBytes))
		if err != nil {
			return 0, nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", "antigravity/1.104.0 darwin/arm64")
		if withProjectHeaders && projectID != "" {
			req.Header.Set("X-Goog-User-Project", projectID)
			req.Header.Set("X-Goog-Project-Id", projectID)
		}
		resp, err := pf.client.Do(req)
		if err != nil {
			return 0, nil, nil, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return resp.StatusCode, body, resp.Header, nil
	}

	statusCode, body, headers, err := doFetch(bodyPayload, true)
	if err != nil {
		return nil, err
	}
	if shouldRetryAntigravityWithoutProjectID(statusCode, body) {
		statusCode, body, headers, err = doFetch(map[string]interface{}{}, false)
		if err != nil {
			return nil, err
		}
	}
	if statusCode == http.StatusTooManyRequests {
		return nil, rateLimitErrorFromHeaders(headers, "antigravity rate limit")
	}
	if statusCode != http.StatusOK {
		diag := ""
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			if scope, exp := fetchGoogleTokenInfo(ctx, pf.client, accessToken); scope != "" || exp != "" {
				diag = fmt.Sprintf(" tokeninfo(scope=%s exp=%s)", scope, exp)
			}
		}
		return nil, fmt.Errorf("antigravity status %d: %s%s", statusCode, strings.TrimSpace(string(body)), diag)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if os.Getenv("QUOTAGUARD_COLLECTOR_DEBUG") == "1" {
		logAntigravityPayload(acc.ID, body)
	}

	if quota := antigravityQuotaFromGroups(acc, payload); quota != nil {
		return quota, nil
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
	dim := models.Dimension{
		Name:       "Antigravity (overall)",
		Type:       models.DimensionSubscription,
		Limit:      limit,
		Used:       used,
		Remaining:  remaining,
		ResetAt:    resetAt,
		Semantics:  models.WindowFixed,
		Source:     models.SourcePolling,
		Confidence: 0.8,
	}
	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Dimensions = models.DimensionSlice{dim}
	quota.Source = models.SourcePolling
	quota.Confidence = 0.8
	quota.CollectedAt = time.Now()
	quota.UpdateEffective()
	return quota, nil
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
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("oauth status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
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

func shouldRetryAntigravityWithoutProjectID(statusCode int, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	text := strings.ToLower(string(body))
	return strings.Contains(text, `unknown name "projectid"`) ||
		strings.Contains(text, `unknown name \"projectid\"`) ||
		strings.Contains(text, "cannot find field")
}

// ---------------- Gemini (OAuth Soft Probe) ----------------

func (pf *ProviderFetcher) fetchGemini(ctx context.Context, acc *models.Account, creds *models.AccountCredentials) (*models.QuotaInfo, error) {
	rawProjectID := strings.TrimSpace(creds.ProjectID)
	if rawProjectID == "" {
		if fallbackEnabled() {
			return geminiEstimatedQuota(acc), nil
		}
		return nil, fmt.Errorf("missing gemini project_id")
	}

	accessToken, err := pf.ensureOAuthToken(ctx, acc.ID, creds, "https://oauth2.googleapis.com/token")
	if err != nil {
		if fallbackEnabled() {
			return geminiEstimatedQuota(acc), nil
		}
		return nil, err
	}
	projectIDs := []string{rawProjectID}
	if strings.Contains(rawProjectID, ",") {
		projectIDs = []string{}
		for _, p := range strings.Split(rawProjectID, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				projectIDs = append(projectIDs, p)
			}
		}
		if len(projectIDs) == 0 {
			return nil, fmt.Errorf("missing gemini project_id")
		}
	}

	location := "us-central1"
	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{{"text": "q"}},
			},
		},
	}
	body, _ := json.Marshal(payload)

	var lastErr error
	for _, projectID := range projectIDs {
		endpoint := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/gemini-2.0-flash:countTokens", location, projectID, location)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := pf.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, rateLimitErrorFromHeaders(resp.Header, "gemini rate limit")
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("gemini status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
			continue
		}

		limit, remaining, resetAt := parseRateLimitHeaders(resp.Header)
		if remaining < 0 {
			// fallback to static limits if headers missing
			limit = 1500
			remaining = 1500
			resetAt = nextMidnightUTC()
		}
		if limit == 0 {
			limit = remaining
		}

		tokensRemaining := parseTokenRemaining(resp.Header)
		return quotaFromRateLimits(acc, limit, remaining, resetAt, tokensRemaining, 0.45), nil
	}

	if lastErr != nil {
		if fallbackEnabled() {
			return geminiEstimatedQuota(acc), nil
		}
		return nil, lastErr
	}
	if fallbackEnabled() {
		return geminiEstimatedQuota(acc), nil
	}
	return nil, fmt.Errorf("gemini request failed")
}

// ---------------- Qwen (OAuth Quota Endpoint) ----------------

func (pf *ProviderFetcher) fetchQwen(ctx context.Context, acc *models.Account, creds *models.AccountCredentials) (*models.QuotaInfo, error) {
	accessToken := strings.TrimSpace(creds.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("missing access_token")
	}
	if creds.ExpiryDateMs > 0 {
		expiry := time.UnixMilli(creds.ExpiryDateMs)
		if time.Now().After(expiry) {
			return nil, fmt.Errorf("qwen token expired")
		}
	}

	endpoints := []string{
		"https://portal.qwen.ai/v1/account/quota",
		"https://dashscope-intl.aliyuncs.com/api/v1/account/quota",
	}
	var lastErr error
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := pf.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, rateLimitErrorFromHeaders(resp.Header, "qwen rate limit")
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("qwen status %d", resp.StatusCode)
			continue
		}

		var result struct {
			RemainingFreeQuota int `json:"remaining_free_quota"`
			DailyRequestLimit  int `json:"daily_request_limit"`
			RequestsUsedToday  int `json:"requests_used_today"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			lastErr = err
			continue
		}
		remaining := result.DailyRequestLimit - result.RequestsUsedToday
		if remaining < 0 {
			remaining = 0
		}
		resetAt := nextMidnightUTC()
		return quotaFromRateLimits(acc, result.DailyRequestLimit, remaining, resetAt, 0, 0.5), nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("qwen quota endpoint failed")
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

func quotaFromRateLimits(acc *models.Account, limit int, remaining int, resetAt *time.Time, tokensRemaining int, confidence float64) *models.QuotaInfo {
	if limit < 0 {
		limit = 0
	}
	if remaining < 0 {
		remaining = 0
	}
	if limit == 0 && remaining > 0 {
		limit = remaining
	}
	used := limit - remaining
	if used < 0 {
		used = 0
	}

	dims := []models.Dimension{
		{
			Type:       models.DimensionRPD,
			Limit:      int64(limit),
			Used:       int64(used),
			Remaining:  int64(remaining),
			ResetAt:    resetAt,
			Semantics:  models.WindowFixed,
			Source:     models.SourceHeaders,
			Confidence: confidence,
		},
	}

	if tokensRemaining > 0 {
		dims = append(dims, models.Dimension{
			Type:       models.DimensionTPD,
			Limit:      int64(tokensRemaining),
			Used:       0,
			Remaining:  int64(tokensRemaining),
			ResetAt:    resetAt,
			Semantics:  models.WindowFixed,
			Source:     models.SourceHeaders,
			Confidence: confidence * 0.8,
		})
	}

	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Dimensions = dims
	quota.Source = models.SourceHeaders
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
	resetAt = parseResetTime(resetVal)
	return limit, remaining, resetAt
}

func parseTokenRemaining(headers http.Header) int {
	value := headerFirst(headers, "x-ratelimit-remaining-tokens", "x-goog-ratelimit-remaining-tokens")
	if value == "" {
		return 0
	}
	return parseInt(value)
}

func parseResetTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if parsed, err := http.ParseTime(value); err == nil {
		return &parsed
	}
	if num, err := strconv.ParseInt(value, 10, 64); err == nil {
		now := time.Now().Unix()
		switch {
		case num > 1_000_000_000_000:
			t := time.UnixMilli(num)
			return &t
		case num > now+3600:
			t := time.Unix(num, 0)
			return &t
		default:
			t := time.Now().Add(time.Duration(num) * time.Second)
			return &t
		}
	}
	return nil
}

func nextMidnightUTC() *time.Time {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return &next
}

type antigravityModelQuota struct {
	Label             string
	Model             string
	RemainingFraction float64
	ResetAt           *time.Time
}

func antigravityQuotaFromGroups(acc *models.Account, payload map[string]interface{}) *models.QuotaInfo {
	modelsQuota := parseAntigravityModelQuotas(payload)
	if len(modelsQuota) == 0 {
		return nil
	}

	type groupStat struct {
		remainingFraction float64
		resetAt           *time.Time
		found             bool
	}

	groupOrder := []string{
		"Gemini 3 Pro (High/Low)",
		"Gemini 3 Flash",
		"Claude 4.5 + Opus 4.5 + GPT OSS 120",
	}
	groups := map[string]*groupStat{
		"Gemini 3 Pro (High/Low)":             {remainingFraction: 1.0},
		"Gemini 3 Flash":                      {remainingFraction: 1.0},
		"Claude 4.5 + Opus 4.5 + GPT OSS 120": {remainingFraction: 1.0},
	}

	for _, mq := range modelsQuota {
		group, ok := antigravityGroupFor(mq.Label, mq.Model)
		if os.Getenv("QUOTAGUARD_COLLECTOR_DEBUG") == "1" {
			log.Printf("antigravity: account=%s model label=%q model=%q group=%q matched=%t", acc.ID, mq.Label, mq.Model, group, ok)
		}
		if !ok {
			continue
		}
		stat := groups[group]
		if !stat.found || mq.RemainingFraction < stat.remainingFraction {
			stat.remainingFraction = mq.RemainingFraction
		}
		if mq.ResetAt != nil {
			if stat.resetAt == nil || mq.ResetAt.Before(*stat.resetAt) {
				t := *mq.ResetAt
				stat.resetAt = &t
			}
		}
		stat.found = true
	}

	dims := make([]models.Dimension, 0, len(groups))
	for _, name := range groupOrder {
		stat := groups[name]
		if !stat.found {
			continue
		}
		remainingPct := stat.remainingFraction * 100
		if remainingPct < 0 {
			remainingPct = 0
		}
		if remainingPct > 100 {
			remainingPct = 100
		}
		limit := int64(100)
		remaining := int64(remainingPct)
		used := limit - remaining
		dims = append(dims, models.Dimension{
			Name:       name,
			Type:       models.DimensionSubscription,
			Limit:      limit,
			Used:       used,
			Remaining:  remaining,
			ResetAt:    stat.resetAt,
			Semantics:  models.WindowFixed,
			Source:     models.SourcePolling,
			Confidence: 0.75,
		})
	}

	if len(dims) == 0 {
		return nil
	}

	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Dimensions = dims
	quota.Source = models.SourcePolling
	quota.Confidence = 0.75
	quota.CollectedAt = time.Now()
	quota.UpdateEffective()
	return quota
}

func antigravityExtractLabelModel(cfg map[string]interface{}) (string, string) {
	label := readString(cfg["label"])
	if label == "" {
		label = readString(cfg["displayName"])
	}
	if label == "" {
		label = readString(cfg["display_name"])
	}
	if label == "" {
		label = readString(cfg["name"])
	}

	model := ""
	if mo, ok := cfg["modelOrAlias"].(map[string]interface{}); ok {
		model = readString(mo["model"])
		if model == "" {
			model = readString(mo["alias"])
		}
	}
	if model == "" {
		model = readString(cfg["model"])
	}
	if model == "" {
		model = readString(cfg["modelId"])
	}
	if model == "" {
		model = readString(cfg["id"])
	}
	if model == "" {
		model = readString(cfg["name"])
	}
	return label, model
}

func antigravityExtractQuotaInfo(cfg map[string]interface{}) map[string]interface{} {
	if q, ok := cfg["quotaInfo"].(map[string]interface{}); ok {
		return q
	}
	if q, ok := cfg["quota_info"].(map[string]interface{}); ok {
		return q
	}
	if q, ok := cfg["quota"].(map[string]interface{}); ok {
		return q
	}
	if q, ok := cfg["quotaStatus"].(map[string]interface{}); ok {
		return q
	}
	if q, ok := cfg["quota_status"].(map[string]interface{}); ok {
		return q
	}
	return nil
}

func parseAntigravityModelQuotas(payload map[string]interface{}) []antigravityModelQuota {
	results := make([]antigravityModelQuota, 0)
	appendQuota := func(label, model string, qinfo map[string]interface{}) {
		remainingFraction, okRemaining := readFloatOK(qinfo["remainingFraction"])
		if !okRemaining {
			remainingFraction, okRemaining = readFloatOK(qinfo["remaining_fraction"])
		}
		if !okRemaining {
			remainingFraction, okRemaining = readFloatOK(qinfo["remainingPercent"])
		}
		if !okRemaining {
			remainingFraction, okRemaining = readFloatOK(qinfo["remaining_pct"])
		}
		if !okRemaining {
			remainingFraction, okRemaining = readFloatOK(qinfo["remaining_percentage"])
		}
		if !okRemaining {
			if rem, okRem := readFloatOK(qinfo["remaining"]); okRem {
				if lim, okLim := readFloatOK(qinfo["limit"]); okLim && lim > 0 {
					remainingFraction = rem / lim
					okRemaining = true
				}
			}
		}
		if !okRemaining {
			return
		}
		if remainingFraction > 1 {
			remainingFraction = remainingFraction / 100
		}
		resetAt := parseResetTime(readString(qinfo["resetTime"]))
		if resetAt == nil {
			resetAt = parseResetTime(readString(qinfo["reset_time"]))
		}
		results = append(results, antigravityModelQuota{
			Label:             label,
			Model:             model,
			RemainingFraction: remainingFraction,
			ResetAt:           resetAt,
		})
	}
	appendFromMap := func(cfg map[string]interface{}) {
		qinfo := antigravityExtractQuotaInfo(cfg)
		if len(qinfo) == 0 {
			return
		}
		label, model := antigravityExtractLabelModel(cfg)
		if label == "" && model == "" {
			return
		}
		appendQuota(label, model, qinfo)
	}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			appendFromMap(t)
			if modelsMap, ok := t["models"].(map[string]interface{}); ok {
				for key, raw := range modelsMap {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					label, model := antigravityExtractLabelModel(cfg)
					if model == "" {
						model = key
					}
					qinfo := antigravityExtractQuotaInfo(cfg)
					if len(qinfo) == 0 {
						continue
					}
					appendQuota(label, model, qinfo)
				}
			}
			if modelsArr, ok := t["models"].([]interface{}); ok {
				for _, raw := range modelsArr {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
				}
			}
			if modelsArr, ok := t["availableModels"].([]interface{}); ok {
				for _, raw := range modelsArr {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
				}
			}
			if modelsArr, ok := t["available_models"].([]interface{}); ok {
				for _, raw := range modelsArr {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
				}
			}
			if modelsArr, ok := t["modelConfigs"].([]interface{}); ok {
				for _, raw := range modelsArr {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
				}
			}
			if modelsArr, ok := t["model_configs"].([]interface{}); ok {
				for _, raw := range modelsArr {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
				}
			}
			if modelsArr, ok := t["availableModelConfigs"].([]interface{}); ok {
				for _, raw := range modelsArr {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
				}
			}
			if modelsArr, ok := t["available_model_configs"].([]interface{}); ok {
				for _, raw := range modelsArr {
					cfg, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
				}
			}
			if arr, ok := t["clientModelConfigs"].([]interface{}); ok {
				for _, item := range arr {
					cfg, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					appendFromMap(cfg)
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
	return results
}

func antigravityGroupFor(label, model string) (string, bool) {
	text := normalizeMatchText(label + " " + model)
	switch {
	case strings.Contains(text, "g3p"),
		(strings.Contains(text, "g3") && strings.Contains(text, "pro")),
		(strings.Contains(text, "gemini") && strings.Contains(text, "pro")):
		return "Gemini 3 Pro (High/Low)", true
	case strings.Contains(text, "g3f"),
		(strings.Contains(text, "g3") && strings.Contains(text, "flash")),
		(strings.Contains(text, "gemini") && strings.Contains(text, "flash")):
		return "Gemini 3 Flash", true
	case strings.Contains(text, "sonnet"),
		strings.Contains(text, "opus"),
		(strings.Contains(text, "claude") && strings.Contains(text, "4 5")),
		(strings.Contains(text, "claude") && strings.Contains(text, "45")),
		(strings.Contains(text, "claude") && strings.Contains(text, "sonnet")):
		return "Claude 4.5 + Opus 4.5 + GPT OSS 120", true
	case strings.Contains(text, "gpt") && strings.Contains(text, "oss") && strings.Contains(text, "120"):
		return "Claude 4.5 + Opus 4.5 + GPT OSS 120", true
	case strings.Contains(text, "gpt oss 120"):
		return "Claude 4.5 + Opus 4.5 + GPT OSS 120", true
	}
	return "", false
}

func normalizeMatchText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return value
	}
	var sb strings.Builder
	sb.Grow(len(value))
	lastSpace := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			sb.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(sb.String())
}

func antigravityCandidateSecrets(current string) []string {
	current = strings.TrimSpace(current)
	if current != "" {
		return []string{current}
	}
	seen := map[string]struct{}{}
	candidates := make([]string, 0, 4)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		candidates = append(candidates, v)
	}
	add("")
	add(os.Getenv("QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET"))
	add(os.Getenv("QUOTAGUARD_GOOGLE_CLIENT_SECRET"))
	for _, part := range strings.Split(os.Getenv("QUOTAGUARD_GOOGLE_CLIENT_SECRET_CANDIDATES"), ",") {
		add(part)
	}
	return candidates
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func logAntigravityPayload(accountID string, body []byte) {
	if len(body) == 0 {
		return
	}
	const maxBytes = 8192
	if len(body) > maxBytes {
		body = body[:maxBytes]
	}
	log.Printf("antigravity: account=%s payload=%s", accountID, strings.TrimSpace(string(body)))
}

func readString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func readFloatOK(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f, true
		}
	case string:
		value := strings.TrimSpace(t)
		if value == "" {
			return 0, false
		}
		value = strings.ReplaceAll(value, "%", "")
		value = strings.ReplaceAll(value, ",", ".")
		value = strings.TrimSpace(value)
		if value == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func fallbackEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("QUOTAGUARD_GEMINI_FALLBACK")))
	return value == "" || value == "1" || value == "true" || value == "on" || value == "static" || value == "lite"
}

func geminiEstimatedQuota(acc *models.Account) *models.QuotaInfo {
	limit := 1500
	remaining := 1500
	resetAt := nextMidnightUTC()

	dim := models.Dimension{
		Name:       "Gemini CLI (estimated)",
		Type:       models.DimensionRPD,
		Limit:      int64(limit),
		Used:       0,
		Remaining:  int64(remaining),
		ResetAt:    resetAt,
		Semantics:  models.WindowFixed,
		Source:     models.SourceEstimated,
		Confidence: 0.2,
	}

	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Dimensions = models.DimensionSlice{dim}
	quota.Source = models.SourceEstimated
	quota.Confidence = 0.2
	quota.CollectedAt = time.Now()
	quota.UpdateEffective()
	return quota
}

func claudeEstimatedQuota(acc *models.Account) *models.QuotaInfo {
	limit := 100
	remaining := 100

	dim := models.Dimension{
		Name:       "Claude Code subscription (estimated)",
		Type:       models.DimensionSubscription,
		Limit:      int64(limit),
		Used:       0,
		Remaining:  int64(remaining),
		Semantics:  models.WindowUnknown,
		Source:     models.SourceEstimated,
		Confidence: 0.15,
	}

	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Dimensions = models.DimensionSlice{dim}
	quota.Source = models.SourceEstimated
	quota.Confidence = 0.15
	quota.CollectedAt = time.Now()
	quota.UpdateEffective()
	return quota
}

func (pf *ProviderFetcher) ensureOAuthToken(ctx context.Context, accountID string, creds *models.AccountCredentials, defaultTokenURI string) (string, error) {
	if creds == nil {
		return "", fmt.Errorf("missing oauth credentials")
	}
	if creds.AccessToken == "" {
		if creds.RefreshToken == "" {
			return "", fmt.Errorf("missing access_token")
		}
	} else if creds.ExpiryDateMs > 0 {
		expiry := time.UnixMilli(creds.ExpiryDateMs)
		if time.Now().Before(expiry.Add(-60 * time.Second)) {
			return creds.AccessToken, nil
		}
	}
	if creds.RefreshToken == "" {
		return creds.AccessToken, nil
	}

	tokenURI := strings.TrimSpace(creds.TokenURI)
	if tokenURI == "" {
		tokenURI = defaultTokenURI
	}

	form := url.Values{}
	form.Set("client_id", creds.ClientID)
	if creds.ClientSecret != "" {
		form.Set("client_secret", creds.ClientSecret)
	}
	form.Set("refresh_token", creds.RefreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(form.Encode()))
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
		return "", rateLimitErrorFromHeaders(resp.Header, "oauth rate limit")
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("oauth status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.AccessToken == "" {
		return "", errors.New("oauth response missing access_token")
	}
	creds.AccessToken = parsed.AccessToken
	if parsed.ExpiresIn > 0 {
		creds.ExpiryDateMs = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second).UnixMilli()
	}
	if pf.store != nil {
		_ = pf.store.SetAccountCredentials(accountID, creds)
	}
	if creds.SourcePath != "" {
		_ = persistOAuthFile(creds.SourcePath, creds)
	}
	return parsed.AccessToken, nil
}

func persistOAuthFile(path string, creds *models.AccountCredentials) error {
	if path == "" {
		return nil
	}
	payload := map[string]interface{}{
		"access_token":  creds.AccessToken,
		"refresh_token": creds.RefreshToken,
		"token_uri":     creds.TokenURI,
		"client_id":     creds.ClientID,
		"client_secret": creds.ClientSecret,
		"expiry_date":   creds.ExpiryDateMs,
		"resource_url":  creds.ResourceURL,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
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

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func fetchGoogleTokenInfo(ctx context.Context, client httpDoer, accessToken string) (string, string) {
	if client == nil || accessToken == "" {
		return "", ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://oauth2.googleapis.com/tokeninfo?access_token="+url.QueryEscape(accessToken), nil)
	if err != nil {
		return "", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	var payload struct {
		Scope     string `json:"scope"`
		ExpiresIn string `json:"expires_in"`
		Exp       string `json:"exp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", ""
	}
	exp := payload.ExpiresIn
	if exp == "" {
		exp = payload.Exp
	}
	return strings.TrimSpace(payload.Scope), strings.TrimSpace(exp)
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

func parseUnixTimePtr(value interface{}) *time.Time {
	switch v := value.(type) {
	case float64:
		if v <= 0 {
			return nil
		}
		t := time.Unix(int64(v), 0)
		return &t
	case int64:
		if v <= 0 {
			return nil
		}
		t := time.Unix(v, 0)
		return &t
	case int:
		if v <= 0 {
			return nil
		}
		t := time.Unix(int64(v), 0)
		return &t
	case json.Number:
		if n, err := v.Int64(); err == nil && n > 0 {
			t := time.Unix(n, 0)
			return &t
		}
	case string:
		if v == "" {
			return nil
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
			t := time.Unix(n, 0)
			return &t
		}
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

var _ QuotaFetcher = (*ProviderFetcher)(nil)
