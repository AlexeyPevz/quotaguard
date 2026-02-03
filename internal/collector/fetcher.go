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
	Response   *antigravityResponseEnvelope `json:"response"`
	UserStatus antigravityUserStatus        `json:"userStatus"`
}

type antigravityResponseEnvelope struct {
	UserStatus antigravityUserStatus `json:"userStatus"`
}

type antigravityUserStatus struct {
	Name                   string                  `json:"name"`
	Email                  string                  `json:"email"`
	PlanStatus             antigravityPlanStatus   `json:"planStatus"`
	CascadeModelConfigData antigravityCascadeModel `json:"cascadeModelConfigData"`
}

type antigravityPlanStatus struct {
	AvailablePromptCredits float64             `json:"availablePromptCredits"`
	AvailableFlowCredits   float64             `json:"availableFlowCredits"`
	PlanInfo               antigravityPlanInfo `json:"planInfo"`
}

type antigravityPlanInfo struct {
	MonthlyPromptCredits float64 `json:"monthlyPromptCredits"`
	MonthlyFlowCredits   float64 `json:"monthlyFlowCredits"`
}

type antigravityCascadeModel struct {
	CascadeConfigList []struct {
		ClientModelConfigs []antigravityModelConfig `json:"clientModelConfigs"`
	} `json:"cascadeConfigList"`
	ClientModelConfigs []antigravityModelConfig `json:"clientModelConfigs"`
}

type antigravityModelConfig struct {
	Label        string `json:"label"`
	ModelOrAlias struct {
		Model string `json:"model"`
	} `json:"modelOrAlias"`
	QuotaInfo struct {
		RemainingFraction float64   `json:"remainingFraction"`
		ResetTime         time.Time `json:"resetTime"`
	} `json:"quotaInfo"`
}

func (pf *ProviderFetcher) fetchAntigravity(ctx context.Context, acc *models.Account, auth *authFile) (*models.QuotaInfo, error) {
	port, csrf, pid, err := pf.resolveAntigravityParams(ctx)
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
	if port == "" && pid != 0 {
		if discovered, derr := pf.discoverAntigravityPort(ctx, pid, csrf, payload); derr == nil {
			port = discovered
		}
	}
	if port == "" {
		return nil, fmt.Errorf("missing antigravity port")
	}

	parsed, err := pf.fetchAntigravityStatus(ctx, port, csrf, payload)
	if err != nil && strings.Contains(err.Error(), "antigravity status 404") && pid != 0 {
		if discovered, derr := pf.discoverAntigravityPort(ctx, pid, csrf, payload); derr == nil && discovered != port {
			parsed, err = pf.fetchAntigravityStatus(ctx, discovered, csrf, payload)
			port = discovered
		}
	}
	if err != nil {
		return nil, err
	}

	remainingPct := 0.0
	resetAt := (*time.Time)(nil)
	found := false

	userStatus := parsed.UserStatus
	if parsed.Response != nil {
		userStatus = parsed.Response.UserStatus
	}

	modelConfigs := userStatus.CascadeModelConfigData.ClientModelConfigs
	if len(modelConfigs) == 0 {
		for _, cfg := range userStatus.CascadeModelConfigData.CascadeConfigList {
			modelConfigs = append(modelConfigs, cfg.ClientModelConfigs...)
		}
	}

	for _, modelCfg := range modelConfigs {
		if modelCfg.QuotaInfo.RemainingFraction <= 0 {
			continue
		}
		if !found || modelCfg.QuotaInfo.RemainingFraction < remainingPct/100 {
			remainingPct = modelCfg.QuotaInfo.RemainingFraction * 100
			resetAt = &modelCfg.QuotaInfo.ResetTime
			found = true
		}
	}

	if !found {
		limit := userStatus.PlanStatus.PlanInfo.MonthlyPromptCredits
		remaining := userStatus.PlanStatus.AvailablePromptCredits
		if limit > 0 && remaining >= 0 {
			remainingPct = (remaining / limit) * 100
			found = true
		}
	}

	responseEmail := strings.TrimSpace(userStatus.Email)
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

func (pf *ProviderFetcher) fetchAntigravityStatus(ctx context.Context, port, csrf string, payload []byte) (*antigravityResponse, error) {
	endpoint := fmt.Sprintf("https://127.0.0.1:%s/exa.language_server_pb.LanguageServerService/GetUserStatus", port)
	resp, err := pf.doAntigravityRequest(ctx, endpoint, csrf, payload, pf.insecureTLS)
	if err != nil {
		if strings.Contains(err.Error(), "http: server gave HTTP response to HTTPS client") {
			httpEndpoint := fmt.Sprintf("http://127.0.0.1:%s/exa.language_server_pb.LanguageServerService/GetUserStatus", port)
			resp, err = pf.doAntigravityRequest(ctx, httpEndpoint, csrf, payload, pf.httpClient)
		}
	}
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
	return &parsed, nil
}

func (pf *ProviderFetcher) doAntigravityRequest(ctx context.Context, url, csrf string, payload []byte, client *http.Client) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Codeium-Csrf-Token", csrf)

	return client.Do(req)
}

func (pf *ProviderFetcher) discoverAntigravityPort(ctx context.Context, pid int, csrf string, payload []byte) (string, error) {
	ports := listListeningPorts(pid)
	if len(ports) == 0 {
		return "", fmt.Errorf("antigravity: no listening ports found for pid %d", pid)
	}

	for _, port := range ports {
		endpoint := fmt.Sprintf("http://127.0.0.1:%s/exa.language_server_pb.LanguageServerService/GetUserStatus", port)
		resp, err := pf.doAntigravityRequest(ctx, endpoint, csrf, payload, pf.httpClient)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			log.Printf("antigravity: discovered api port %s (pid %d)", port, pid)
			return port, nil
		}
	}

	return "", fmt.Errorf("antigravity: unable to probe api port for pid %d", pid)
}

func listListeningPorts(pid int) []string {
	inodes := map[string]struct{}{}
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	fdEntries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil
	}
	for _, entry := range fdEntries {
		link, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if inode != "" {
				inodes[inode] = struct{}{}
			}
		}
	}

	var ports []string
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if i == 0 || strings.TrimSpace(line) == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			inode := fields[9]
			if _, ok := inodes[inode]; !ok {
				continue
			}
			local := fields[1]
			state := fields[3]
			if state != "0A" {
				continue
			}
			parts := strings.Split(local, ":")
			if len(parts) != 2 {
				continue
			}
			portHex := parts[1]
			port, err := strconv.ParseInt(portHex, 16, 32)
			if err != nil || port <= 0 {
				continue
			}
			ports = append(ports, strconv.Itoa(int(port)))
		}
	}

	return ports
}

func (pf *ProviderFetcher) resolveAntigravityParams(ctx context.Context) (string, string, int, error) {
	port := os.Getenv("QUOTAGUARD_ANTIGRAVITY_PORT")
	csrf := os.Getenv("QUOTAGUARD_ANTIGRAVITY_CSRF")
	if port != "" && csrf != "" {
		return port, csrf, 0, nil
	}

	foundPort, foundCSRF, pid := scanAntigravityProcess()
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
				foundPort, foundCSRF, pid = scanAntigravityProcess()
				if port == "" && foundPort != "" {
					port = foundPort
					log.Printf("antigravity: detected port after start %s", port)
				}
				if csrf == "" && foundCSRF != "" {
					csrf = foundCSRF
					log.Printf("antigravity: detected csrf after start")
				}
				if port != "" && csrf != "" {
					return port, csrf, pid, nil
				}
				select {
				case <-ctx.Done():
					return "", "", 0, ctx.Err()
				case <-time.After(500 * time.Millisecond):
				}
			}
		}
	}

	if csrf == "" {
		return port, "", pid, fmt.Errorf("missing QUOTAGUARD_ANTIGRAVITY_CSRF (auto-detect failed)")
	}
	return port, csrf, pid, nil
}

func (pf *ProviderFetcher) startAntigravityServer(ctx context.Context) bool {
	cmdLine := strings.TrimSpace(os.Getenv("QUOTAGUARD_ANTIGRAVITY_START_CMD"))
	if cmdLine == "" {
		return pf.startAntigravityFromPath(ctx)
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

func (pf *ProviderFetcher) startAntigravityFromPath(ctx context.Context) bool {
	antigravityPath, err := exec.LookPath("antigravity")
	if err != nil {
		return false
	}

	root := antigravityPath
	for i := 0; i < 4; i++ {
		root = filepath.Dir(root)
	}
	if root == "" || root == "/" {
		return false
	}

	serverPath := filepath.Join(root, "bin", "antigravity-server")
	if _, err := os.Stat(serverPath); err != nil {
		return false
	}

	hash := filepath.Base(root)
	parent := filepath.Dir(filepath.Dir(root))
	tokenFile := filepath.Join(parent, "."+hash+".token")

	args := []string{
		"--start-server",
		"--host=127.0.0.1",
		"--port=0",
		"--connection-token-file", tokenFile,
		"--telemetry-level", "off",
		"--enable-remote-auto-shutdown",
		"--accept-server-license-terms",
	}

	cmd := exec.CommandContext(ctx, serverPath, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		log.Printf("antigravity: auto-start failed: %v", err)
		return false
	}
	log.Printf("antigravity: auto-started via %s", serverPath)
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
	antigravityPortRegexp = regexp.MustCompile(`--(?:port|extension_server_port|extension-server-port)(?:=|\s+)(\d{4,5})`)
	antigravityCSRFRegexp = regexp.MustCompile(`(?i)csrf(?:_token|-token)?(?:=|\s+)([A-Za-z0-9_-]{10,})`)
)

func scanAntigravityProcess() (string, string, int) {
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		return "", "", 0
	}

	var port, csrf string
	var pid int

	for _, entry := range procEntries {
		if !entry.IsDir() || !isNumeric(entry.Name()) {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		cmd := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !strings.Contains(cmd, "language_server") && !strings.Contains(cmd, "codeium") {
			continue
		}
		isLanguageServer := strings.Contains(cmd, "language_server")

		if port == "" {
			if match := antigravityPortRegexp.FindStringSubmatch(cmd); len(match) > 1 {
				port = match[1]
			} else if match := regexp.MustCompile(`127\.0\.0\.1:(\d{4,5})`).FindStringSubmatch(cmd); len(match) > 1 {
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

		if pid == 0 && isLanguageServer {
			if parsedPID, err := strconv.Atoi(entry.Name()); err == nil {
				pid = parsedPID
			}
		}

		if port != "" && csrf != "" {
			return port, csrf, pid
		}
	}

	return port, csrf, pid
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
