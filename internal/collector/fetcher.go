package collector

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// ProviderFetcher implements QuotaFetcher for multiple providers.
type ProviderFetcher struct {
	store       store.Store
	httpClient  *http.Client
	insecureTLS *http.Client
}

// NewProviderFetcher creates a new provider-aware fetcher.
func NewProviderFetcher(s store.Store) *ProviderFetcher {
	return &ProviderFetcher{
		store:      s,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		insecureTLS: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// FetchQuota fetches quota for a given account ID.
func (pf *ProviderFetcher) FetchQuota(ctx context.Context, accountID string) (*models.QuotaInfo, error) {
	acc, ok := pf.store.GetAccount(accountID)
	if !ok {
		return nil, fmt.Errorf("account not found: %s", accountID)
	}

	auth, err := pf.loadAuth(acc.CredentialsRef)
	if err != nil {
		return nil, err
	}

	switch auth.Type {
	case "antigravity":
		return pf.fetchAntigravity(ctx, acc, auth)
	case "codex":
		return pf.fetchCodex(ctx, acc, auth)
	case "gemini":
		return pf.fetchGeminiCLI(ctx, acc, auth)
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", auth.Type)
	}
}

type authFile struct {
	AccessToken  string `json:"access_token"`
	Email        string `json:"email"`
	Type         string `json:"type"`
	SessionToken string `json:"session_token,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Timestamp    int64  `json:"timestamp"`
	Path         string `json:"-"`
}

func (pf *ProviderFetcher) loadAuth(path string) (*authFile, error) {
	if path == "" {
		return nil, fmt.Errorf("credentials_ref is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read auth file: %w", err)
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("parse auth file: %w", err)
	}
	auth.Path = path
	return &auth, nil
}

// ---------------- Antigravity ----------------

type antigravityRequest struct {
	Metadata struct {
		APIVersion       string `json:"api_version"`
		IDEName          string `json:"ide_name"`
		IDEVersion       string `json:"ide_version"`
		ExtensionName    string `json:"extension_name"`
		ExtensionVersion string `json:"extension_version"`
	} `json:"metadata"`
}

type antigravityResponse struct {
	Response struct {
		UserStatus struct {
			Name                   string `json:"name"`
			Email                  string `json:"email"`
			CascadeModelConfigData struct {
				CascadeConfigList []struct {
					ClientModelConfigs []struct {
						Label        string `json:"label"`
						ModelOrAlias struct {
							Model string `json:"model"`
						} `json:"modelOrAlias"`
						QuotaInfo struct {
							RemainingFraction float64   `json:"remainingFraction"`
							ResetTime         time.Time `json:"resetTime"`
						} `json:"quotaInfo"`
					} `json:"clientModelConfigs"`
				} `json:"cascadeConfigList"`
			} `json:"cascadeModelConfigData"`
		} `json:"userStatus"`
	} `json:"response"`
}

func (pf *ProviderFetcher) fetchAntigravity(ctx context.Context, acc *models.Account, auth *authFile) (*models.QuotaInfo, error) {
	port, csrf, err := pf.resolveAntigravityParams(ctx)
	if err != nil {
		return nil, err
	}

	reqBody := antigravityRequest{}
	reqBody.Metadata.APIVersion = "1.0.0"
	reqBody.Metadata.IDEName = "VSCode"
	reqBody.Metadata.IDEVersion = "1.80.0"
	reqBody.Metadata.ExtensionName = "QuotaGuard"
	reqBody.Metadata.ExtensionVersion = "0.1.0"

	payload, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("https://127.0.0.1:%s/exa.language_server_pb.LanguageServerService/GetUserStatus", port)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Codeium-Csrf-Token", csrf)

	resp, err := pf.insecureTLS.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("antigravity status %d: %s", resp.StatusCode, string(body))
	}

	var parsed antigravityResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	remainingPct := 0.0
	resetAt := (*time.Time)(nil)
	found := false

	for _, cfg := range parsed.Response.UserStatus.CascadeModelConfigData.CascadeConfigList {
		for _, modelCfg := range cfg.ClientModelConfigs {
			if !found || modelCfg.QuotaInfo.RemainingFraction < remainingPct/100 {
				remainingPct = modelCfg.QuotaInfo.RemainingFraction * 100
				resetAt = &modelCfg.QuotaInfo.ResetTime
				found = true
			}
		}
	}

	responseEmail := strings.TrimSpace(parsed.Response.UserStatus.Email)
	if responseEmail != "" && auth.Email != "" && !strings.EqualFold(responseEmail, auth.Email) {
		return nil, fmt.Errorf("antigravity session email mismatch: got %s expected %s", responseEmail, auth.Email)
	}

	if !found {
		return nil, errors.New("antigravity quota not found")
	}

	remainingInt := int64(remainingPct)
	if remainingInt < 0 {
		remainingInt = 0
	}
	if remainingInt > 100 {
		remainingInt = 100
	}
	dim := models.Dimension{
		Type:       models.DimensionSubscription,
		Limit:      100,
		Remaining:  remainingInt,
		Used:       100 - remainingInt,
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

func (pf *ProviderFetcher) resolveAntigravityParams(ctx context.Context) (string, string, error) {
	port := os.Getenv("QUOTAGUARD_ANTIGRAVITY_PORT")
	csrf := os.Getenv("QUOTAGUARD_ANTIGRAVITY_CSRF")
	if port != "" && csrf != "" {
		return port, csrf, nil
	}

	foundPort, foundCSRF := scanAntigravityProcess()
	if port == "" && foundPort != "" {
		port = foundPort
		log.Printf("antigravity: auto-detected port %s", port)
	}
	if csrf == "" && foundCSRF != "" {
		csrf = foundCSRF
		log.Printf("antigravity: auto-detected csrf token")
	}

	if port == "" || csrf == "" {
		if pf.startAntigravityServer(ctx) {
			timeout := parseDurationEnv("QUOTAGUARD_ANTIGRAVITY_START_TIMEOUT", 15*time.Second)
			deadline := time.Now().Add(timeout)
			for time.Now().Before(deadline) {
				foundPort, foundCSRF = scanAntigravityProcess()
				if port == "" && foundPort != "" {
					port = foundPort
					log.Printf("antigravity: detected port after start %s", port)
				}
				if csrf == "" && foundCSRF != "" {
					csrf = foundCSRF
					log.Printf("antigravity: detected csrf after start")
				}
				if port != "" && csrf != "" {
					return port, csrf, nil
				}
				select {
				case <-ctx.Done():
					return "", "", ctx.Err()
				case <-time.After(500 * time.Millisecond):
				}
			}
		}
	}

	if port == "" {
		return "", "", fmt.Errorf("missing QUOTAGUARD_ANTIGRAVITY_PORT (auto-detect failed)")
	}
	if csrf == "" {
		return "", "", fmt.Errorf("missing QUOTAGUARD_ANTIGRAVITY_CSRF (auto-detect failed)")
	}
	return port, csrf, nil
}

func (pf *ProviderFetcher) startAntigravityServer(ctx context.Context) bool {
	cmdLine := strings.TrimSpace(os.Getenv("QUOTAGUARD_ANTIGRAVITY_START_CMD"))
	if cmdLine == "" {
		return false
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdLine)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		log.Printf("antigravity: start command failed: %v", err)
		return false
	}
	log.Printf("antigravity: start command launched")
	return true
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

var (
	antigravityPortRegexp = regexp.MustCompile(`--port(?:=|\\s+)(\\d{4,5})`)
	antigravityCSRFRegexp = regexp.MustCompile(`(?i)csrf(?:=|\\s+)([A-Za-z0-9_-]{10,})`)
)

func scanAntigravityProcess() (string, string) {
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		return "", ""
	}

	var port, csrf string

	for _, entry := range procEntries {
		if !entry.IsDir() || !isNumeric(entry.Name()) {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		cmd := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !strings.Contains(cmd, "language_server") && !strings.Contains(cmd, "antigravity") && !strings.Contains(cmd, "codeium") {
			continue
		}

		if port == "" {
			if match := antigravityPortRegexp.FindStringSubmatch(cmd); len(match) > 1 {
				port = match[1]
			} else if match := regexp.MustCompile(`127\\.0\\.0\\.1:(\\d{4,5})`).FindStringSubmatch(cmd); len(match) > 1 {
				port = match[1]
			}
		}

		if csrf == "" {
			if match := antigravityCSRFRegexp.FindStringSubmatch(cmd); len(match) > 1 {
				csrf = match[1]
			}
		}

		if csrf == "" {
			envData, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
			if err == nil {
				env := strings.Split(string(envData), "\x00")
				for _, kv := range env {
					if strings.HasPrefix(kv, "CODEIUM_CSRF_TOKEN=") || strings.HasPrefix(kv, "X_CODEIUM_CSRF_TOKEN=") {
						parts := strings.SplitN(kv, "=", 2)
						if len(parts) == 2 && parts[1] != "" {
							csrf = parts[1]
							break
						}
					}
				}
			}
		}

		if port != "" && csrf != "" {
			return port, csrf
		}
	}

	return port, csrf
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ---------------- Codex (ChatGPT) ----------------

type codexSessionResponse struct {
	AccessToken string `json:"accessToken"`
	User        struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
}

func (pf *ProviderFetcher) fetchCodex(ctx context.Context, acc *models.Account, auth *authFile) (*models.QuotaInfo, error) {
	sessionToken := auth.SessionToken
	if sessionToken == "" {
		sessionToken = pf.loadCodexSessionToken()
	}
	if sessionToken == "" {
		return nil, fmt.Errorf("missing codex session_token")
	}

	jwt, accountID, err := pf.codexJWT(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	usage, err := pf.codexUsage(ctx, jwt, accountID)
	if err != nil {
		return nil, err
	}

	limit, used := parseCodexUsage(usage)
	if limit <= 0 {
		return nil, fmt.Errorf("codex usage response missing limits")
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
		Semantics:  models.WindowFixed,
		Source:     models.SourcePolling,
		Confidence: 0.7,
	}

	quota := models.NewQuotaInfo()
	quota.Provider = acc.Provider
	quota.AccountID = acc.ID
	quota.Tier = acc.Tier
	quota.Dimensions = models.DimensionSlice{dim}
	quota.Source = models.SourcePolling
	quota.Confidence = 0.7
	quota.CollectedAt = time.Now()
	quota.UpdateEffective()

	return quota, nil
}

func (pf *ProviderFetcher) loadCodexSessionToken() string {
	if pf.store != nil {
		settings := pf.store.Settings()
		if settings != nil {
			if token, ok := settings.Get(store.SettingCodexSessionToken); ok && strings.TrimSpace(token) != "" {
				return strings.TrimSpace(token)
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".codex", "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var auth authFile
	if json.Unmarshal(data, &auth) != nil {
		return ""
	}
	return auth.SessionToken
}

func (pf *ProviderFetcher) codexJWT(ctx context.Context, sessionToken string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/api/auth/session", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Cookie", "__Secure-next-auth.session-token="+sessionToken)

	resp, err := pf.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("codex session status %d: %s", resp.StatusCode, string(body))
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

func (pf *ProviderFetcher) codexUsage(ctx context.Context, jwtToken, accountID string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("ChatGPT-Account-Id", accountID)
	req.Header.Set("User-Agent", "QuotaGuard/0.1.0")
	req.Header.Set("OAI-Language", "en-US")

	resp, err := pf.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("codex usage status %d: %s", resp.StatusCode, string(body))
	}

	var parsed map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func parseCodexUsage(payload map[string]interface{}) (limit int, used int) {
	// Try to find usage.codex_cli.requests_used/limit
	if usage, ok := payload["usage"].(map[string]interface{}); ok {
		if codex, ok := usage["codex_cli"].(map[string]interface{}); ok {
			used = int(readFloat(codex["requests_used"]))
			limit = int(readFloat(codex["requests_limit"]))
			if limit > 0 {
				return limit, used
			}
		}
	}
	return 0, 0
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

// ---------------- Gemini CLI ----------------

var geminiStatsRegexp = regexp.MustCompile(`(?i)tokens used:\\s*([0-9,]+)`)

func (pf *ProviderFetcher) fetchGeminiCLI(ctx context.Context, acc *models.Account, auth *authFile) (*models.QuotaInfo, error) {
	_ = auth
	cmd := exec.CommandContext(ctx, "gemini", "/stats")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gemini /stats failed: %w", err)
	}

	used := int64(0)
	matches := geminiStatsRegexp.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		parsed := strings.ReplaceAll(matches[1], ",", "")
		if v, err := strconv.ParseInt(parsed, 10, 64); err == nil {
			used = v
		}
	}

	// CLI does not expose limit; provide estimated dimension with low confidence.
	dim := models.Dimension{
		Type:       models.DimensionSubscription,
		Limit:      100,
		Used:       minInt64(used, 100),
		Remaining:  maxInt64(0, 100-used),
		Semantics:  models.WindowUnknown,
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

	return quota, nil
}

func minInt64(a, b int64) int64 {
	if a < b {
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
