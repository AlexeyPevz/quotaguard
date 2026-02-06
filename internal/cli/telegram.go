package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quotaguard/quotaguard/internal/cliproxy"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/quotaguard/quotaguard/internal/telegram"
)

const accountDisableUntilKeyPrefix = "router.account_disable_until."

const (
	googleOAuthAuthURL        = "https://accounts.google.com/o/oauth2/auth"
	googleOAuthTokenURL       = "https://oauth2.googleapis.com/token"
	googleOAuthUserInfoURL    = "https://www.googleapis.com/oauth2/v2/userinfo"
	defaultOAuthCallbackURI   = "http://localhost:1456/oauth-callback"
)

var (
	antigravityOAuthScopes = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
		"https://www.googleapis.com/auth/cclog",
		"https://www.googleapis.com/auth/experimentsandconfigs",
	}
	geminiOAuthScopes = []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	}
)

func setupTelegramBot(cfg *config.Config, settings store.SettingsStore, s store.Store, routerSvc router.Router, accountManager *cliproxy.AccountManager, loader *config.Loader, routerCfg router.Config) (*telegram.Bot, error) {
	if cfg == nil || !cfg.Telegram.Enabled {
		return nil, nil
	}
	if cfg.Telegram.BotToken == "" {
		return nil, fmt.Errorf("telegram enabled but bot token is empty")
	}

	apiClient, err := telegram.NewTGBotAPIClient(cfg.Telegram.BotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram client: %w", err)
	}

	options := &telegram.BotOptions{
		BotAPI:   apiClient,
		Settings: settings,
	}
	if cfg.Telegram.RateLimit.MessagesPerMinute > 0 {
		options.RateLimiter = telegram.NewRateLimiter(cfg.Telegram.RateLimit.MessagesPerMinute)
	}

	bot := telegram.NewBot(cfg.Telegram.BotToken, cfg.Telegram.ChatID, cfg.Telegram.Enabled, options)
	if accountManager != nil {
		bot.SetAutoImportEnabled(true)
	}

	bot.SetStatusCallback(func() (*telegram.SystemStatus, error) {
		active := len(s.ListEnabledAccounts())
		status := "unhealthy"
		if routerSvc.IsHealthy() {
			status = "healthy"
		}
		return &telegram.SystemStatus{
			AccountsActive: active,
			RouterStatus:   status,
			AvgLatency:     0,
			LastUpdate:     time.Now(),
		}, nil
	})

	bot.SetQuotasCallback(func() ([]telegram.AccountQuota, error) {
		reconcileTemporaryDisables(s, settings)
		warnUsageThreshold := routerCfg.WarningThreshold
		if routerSvc != nil {
			if cfg := routerSvc.GetConfig(); cfg != nil {
				warnUsageThreshold = cfg.WarningThreshold
			}
		}
		warnRemainingThreshold := 100.0 - warnUsageThreshold
		activeAccountID := ""
		if routerSvc != nil {
			activeAccountID = routerSvc.GetCurrentAccount()
		}

		enabled := make(map[string]bool)
		providerType := make(map[string]string)
		emails := make(map[string]string)
		hasCreds := make(map[string]bool)
		for _, acc := range s.ListAccounts() {
			if acc == nil {
				continue
			}
			enabled[acc.ID] = acc.Enabled
			if acc.ProviderType != "" {
				providerType[acc.ID] = acc.ProviderType
			}
			if acc.ProviderType != "" && acc.CredentialsRef != "" {
				hasCreds[acc.ID] = true
			}
			if creds, ok := s.GetAccountCredentials(acc.ID); ok {
				if creds.Email != "" {
					emails[acc.ID] = creds.Email
				}
			}
		}

		quotas := s.ListQuotas()
		result := make([]telegram.AccountQuota, 0, len(quotas))
		for _, quota := range quotas {
			if quota == nil {
				continue
			}
			if ok, exists := enabled[quota.AccountID]; exists && !ok {
				continue
			}
			if ok := hasCreds[quota.AccountID]; !ok {
				continue
			}
			activity, _ := s.GetAccountActivity(quota.AccountID)
			groupLastUse := map[string]time.Time{}
			var accountLastUse *time.Time
			if activity != nil {
				groupLastUse = activity.GroupLastUse
				accountLastUse = activity.AccountLastUse
			}
			remaining := quota.EffectiveRemainingWithVirtual()
			breakdown := make([]telegram.QuotaBreakdown, 0)
			var accountResetAt *time.Time
			for _, dim := range quota.Dimensions {
				if dim.Name == "" {
					continue
				}
				detailRemaining := dim.RemainingPercent()
				if dim.ResetAt != nil {
					accountResetAt = minTimePtr(accountResetAt, dim.ResetAt)
				}
				lastCallAt := accountLastUse
				if ts, ok := groupLastUse[dim.Name]; ok {
					lastCallAt = nullableTime(ts)
				}
				breakdown = append(breakdown, telegram.QuotaBreakdown{
					Name:         dim.Name,
					UsagePercent: detailRemaining,
					IsWarning:    detailRemaining <= warnRemainingThreshold,
					ResetAt:      dim.ResetAt,
					LastCallAt:   lastCallAt,
					IsActive:     quota.AccountID == activeAccountID,
				})
			}
			providerLabel := string(quota.Provider)
			if pt := providerType[quota.AccountID]; pt != "" {
				providerLabel = pt
			}
			lastCallAt := accountLastUse
			if lastCallAt == nil {
				lastCallAt = nullableTime(quota.CollectedAt)
			}
			result = append(result, telegram.AccountQuota{
				AccountID:    quota.AccountID,
				Provider:     providerLabel,
				Email:        emails[quota.AccountID],
				UsagePercent: remaining,
				IsWarning:    remaining <= warnRemainingThreshold,
				Breakdown:    breakdown,
				IsActive:     quota.AccountID == activeAccountID,
				ResetAt:      accountResetAt,
				LastCallAt:   lastCallAt,
			})
		}
		return result, nil
	})

	bot.SetAccountsCallback(func() ([]telegram.AccountControl, error) {
		reconcileTemporaryDisables(s, settings)
		activeAccountID := ""
		if routerSvc != nil {
			activeAccountID = routerSvc.GetCurrentAccount()
		}
		accounts := s.ListAccounts()
		rows := make([]telegram.AccountControl, 0, len(accounts))
		for _, acc := range accounts {
			if acc == nil {
				continue
			}
			if acc.ProviderType == "" || acc.CredentialsRef == "" {
				continue
			}
			row := telegram.AccountControl{
				AccountID: acc.ID,
				Provider:  acc.ProviderType,
				Enabled:   acc.Enabled,
				IsActive:  acc.ID == activeAccountID,
			}
			if creds, ok := s.GetAccountCredentials(acc.ID); ok && creds.Email != "" {
				row.Email = creds.Email
			}
			if until := getDisableUntil(settings, acc.ID); until != nil && until.After(time.Now()) && !acc.Enabled {
				row.DisabledUntil = until
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool {
			pi := strings.ToLower(rows[i].Provider)
			pj := strings.ToLower(rows[j].Provider)
			if pi != pj {
				return pi < pj
			}
			return rows[i].AccountID < rows[j].AccountID
		})
		return rows, nil
	})

	bot.SetToggleAccountCallback(func(accountID string, duration time.Duration, enable bool) error {
		acc, ok := s.GetAccount(accountID)
		if !ok || acc == nil {
			return fmt.Errorf("account not found: %s", accountID)
		}
		now := time.Now()
		if enable {
			acc.Enabled = true
			acc.BlockedUntil = nil
			acc.UpdatedAt = now
			s.SetAccount(acc)
			_ = s.SetAccountBlockedUntil(accountID, nil)
			clearDisableUntil(settings, accountID)
			return nil
		}

		acc.Enabled = false
		disabledUntil := now.Add(duration)
		acc.BlockedUntil = &disabledUntil
		acc.UpdatedAt = now
		s.SetAccount(acc)
		_ = s.SetAccountBlockedUntil(accountID, &disabledUntil)
		setDisableUntil(settings, accountID, disabledUntil)
		return nil
	})

	bot.SetAlertsCallback(func() ([]telegram.ActiveAlert, error) {
		return []telegram.ActiveAlert{}, nil
	})

	bot.SetAccountCheckConfigCallbacks(
		func() (*telegram.AccountCheckConfig, error) {
			interval, timeout := resolveAccountCheckConfig(
				settings,
				envDuration("QUOTAGUARD_ACCOUNT_CHECK_INTERVAL", 3*time.Minute),
				envDuration("QUOTAGUARD_ACCOUNT_CHECK_TIMEOUT", 12*time.Second),
			)
			return &telegram.AccountCheckConfig{
				Interval: interval,
				Timeout:  timeout,
			}, nil
		},
		func(interval, timeout time.Duration) error {
			interval, timeout = resolveAccountCheckConfig(settings, interval, timeout)
			if settings == nil {
				return fmt.Errorf("settings store not configured")
			}
			if err := settings.SetInt(store.SettingAccountCheckIntSec, int(interval.Seconds())); err != nil {
				return err
			}
			if err := settings.SetInt(store.SettingAccountCheckTOSec, int(timeout.Seconds())); err != nil {
				return err
			}
			return nil
		},
	)

	bot.SetLoginCallbacks(
		func(provider string, _ int64) (*telegram.LoginURLPayload, error) {
			spec, err := oauthProviderSpec(provider)
			if err != nil {
				return nil, err
			}
			state, err := newOAuthState()
			if err != nil {
				return nil, err
			}
			redirectURI := oauthRedirectURI()
			values := url.Values{}
			values.Set("access_type", "offline")
			values.Set("client_id", spec.ClientID)
			values.Set("prompt", "consent")
			values.Set("redirect_uri", redirectURI)
			values.Set("response_type", "code")
			values.Set("scope", strings.Join(spec.Scopes, " "))
			values.Set("state", state)

			return &telegram.LoginURLPayload{
				Provider: provider,
				State:    state,
				URL:      googleOAuthAuthURL + "?" + values.Encode(),
				Instructions: "Откроется Google OAuth. После редиректа скопируй URL из адресной строки " +
					"и отправь его сюда.",
			}, nil
		},
		func(provider, _ string, code string, _ int64) (*telegram.LoginResult, error) {
			spec, err := oauthProviderSpec(provider)
			if err != nil {
				return nil, err
			}
			token, err := exchangeGoogleOAuthCode(context.Background(), spec, code)
			if err != nil {
				return nil, err
			}
			email, err := fetchGoogleUserEmail(context.Background(), token.AccessToken)
			if err != nil {
				return nil, err
			}
			accountID, existingID := findOrBuildAccountID(s, spec.ProviderType, email)
			if existingID != "" {
				accountID = existingID
				if token.RefreshToken == "" {
					if existingCreds, ok := s.GetAccountCredentials(existingID); ok && existingCreds != nil {
						token.RefreshToken = strings.TrimSpace(existingCreds.RefreshToken)
					}
				}
			}
			authPath := buildProviderAuthPath(accountManager, spec.ProviderType, email)
			now := time.Now()
			account := &models.Account{
				ID:             accountID,
				Provider:       spec.Provider,
				ProviderType:   spec.ProviderType,
				Enabled:        true,
				Priority:       spec.Priority,
				CredentialsRef: authPath,
				OAuthCredsPath: authPath,
				UpdatedAt:      now,
			}
			if existing, ok := s.GetAccount(accountID); ok && existing != nil {
				account.Priority = existing.Priority
				account.Enabled = existing.Enabled
				account.Tier = existing.Tier
				account.CreatedAt = existing.CreatedAt
			}
			s.SetAccount(account)

			rawPayload := map[string]interface{}{
				"type":          spec.ProviderType,
				"email":         email,
				"access_token":  token.AccessToken,
				"refresh_token": token.RefreshToken,
				"token_uri":     googleOAuthTokenURL,
				"client_id":     spec.ClientID,
				"client_secret": spec.ClientSecret,
				"expiry_date":   token.ExpiryDateMs,
				"timestamp":     now.UnixMilli(),
				"expires_in":    token.ExpiresIn,
				"expired":       now.Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339),
				"auth_method":   "telegram_oauth",
			}
			rawJSON, _ := json.Marshal(rawPayload)
			creds := &models.AccountCredentials{
				Type:         spec.ProviderType,
				Email:        email,
				AccessToken:  token.AccessToken,
				RefreshToken: token.RefreshToken,
				ClientID:     spec.ClientID,
				ClientSecret: spec.ClientSecret,
				TokenURI:     googleOAuthTokenURL,
				ExpiryDateMs: token.ExpiryDateMs,
				SourcePath:   authPath,
				Raw:          string(rawJSON),
			}
			if err := s.SetAccountCredentials(accountID, creds); err != nil {
				return nil, err
			}
			if err := persistProviderAuthFile(authPath, spec.ProviderType, email, token, spec); err != nil {
				return nil, err
			}
			return &telegram.LoginResult{
				AccountID: accountID,
				Email:     email,
				Provider:  spec.ProviderType,
			}, nil
		},
	)

	bot.SetThresholdsCallback(func(warning, switchVal, critical float64) error {
		if routerSvc == nil {
			return nil
		}
		current := routerSvc.GetConfig()
		if current == nil {
			return nil
		}
		newCfg := *current
		newCfg.WarningThreshold = warning
		newCfg.SwitchThreshold = switchVal
		newCfg.CriticalThreshold = critical
		routerSvc.UpdateConfig(newCfg)
		return nil
	})

	bot.SetPolicyCallback(func(policy string) error {
		if routerSvc == nil {
			return nil
		}
		current := routerSvc.GetConfig()
		if current == nil {
			return nil
		}
		newCfg := *current
		newCfg.DefaultPolicy = policy
		routerSvc.UpdateConfig(newCfg)
		return nil
	})

	bot.SetFallbackCallback(func(chains map[string][]string) error {
		if routerSvc == nil {
			return nil
		}
		current := routerSvc.GetConfig()
		if current == nil {
			return nil
		}
		newCfg := *current
		newCfg.FallbackChains = chains
		routerSvc.UpdateConfig(newCfg)
		return nil
	})

	bot.SetIgnoreEstimatedCallback(func(ignore bool) error {
		if routerSvc == nil {
			return nil
		}
		current := routerSvc.GetConfig()
		if current == nil {
			return nil
		}
		newCfg := *current
		newCfg.IgnoreEstimated = ignore
		routerSvc.UpdateConfig(newCfg)
		return nil
	})

	bot.SetRouterConfigCallback(func() (*telegram.RouterConfig, error) {
		if routerSvc == nil {
			return nil, nil
		}
		current := routerSvc.GetConfig()
		if current == nil {
			return nil, nil
		}
		return &telegram.RouterConfig{
			WarningThreshold:  current.WarningThreshold,
			SwitchThreshold:   current.SwitchThreshold,
			CriticalThreshold: current.CriticalThreshold,
			DefaultPolicy:     current.DefaultPolicy,
			IgnoreEstimated:   current.IgnoreEstimated,
			FallbackChains:    current.FallbackChains,
		}, nil
	})

	bot.SetImportCallback(func(path string) (int, int, error) {
		var newCount, updatedCount int
		if accountManager == nil {
			if path != "" {
				return 0, 0, fmt.Errorf("account manager not initialized")
			}
		} else {
			if path != "" {
				accountManager = cliproxy.NewAccountManager(s, path, 5*time.Minute)
			}
			n, u, err := accountManager.ScanAndSync()
			if err != nil {
				return 0, 0, err
			}
			newCount += n
			updatedCount += u
		}
		oauthNew, oauthUpdated, _ := importOAuthCredentials(s)
		return newCount + oauthNew, updatedCount + oauthUpdated, nil
	})

	bot.SetExportCallback(func() (string, error) {
		data, err := os.ReadFile(globalFlags.Config)
		if err != nil {
			return "", err
		}
		return string(data), nil
	})

	bot.SetReloadCallback(func() error {
		if loader == nil {
			return fmt.Errorf("config loader not initialized")
		}
		newCfg, err := loader.Reload()
		if err != nil {
			return err
		}
		if err := ensureSettingsDefaults(settings, newCfg); err != nil {
			return err
		}
		applySettingsToTelegramConfig(settings, &newCfg.Telegram)

		newRouterCfg := router.Config{
			WarningThreshold:    newCfg.Router.Thresholds.Warning,
			SwitchThreshold:     newCfg.Router.Thresholds.Switch,
			CriticalThreshold:   newCfg.Router.Thresholds.Critical,
			MinSafeThreshold:    newCfg.Router.Thresholds.MinSafe,
			MinDwellTime:        newCfg.Router.AntiFlapping.MinDwellTime,
			CooldownAfterSwitch: newCfg.Router.AntiFlapping.CooldownAfterSwitch,
			HysteresisMargin:    newCfg.Router.AntiFlapping.HysteresisMargin,
			IgnoreEstimated:     newCfg.Router.IgnoreEstimated,
			Weights: router.Weights{
				Safety:      newCfg.Router.Weights.Safety,
				Refill:      newCfg.Router.Weights.Refill,
				Tier:        newCfg.Router.Weights.Tier,
				Reliability: newCfg.Router.Weights.Reliability,
				Cost:        newCfg.Router.Weights.Cost,
			},
			DefaultPolicy:  "balanced",
			Policies:       buildRouterPolicyMap(&newCfg.Router),
			FallbackChains: newCfg.Router.FallbackChains,
		}
		if err := applySettingsToRouterConfig(settings, &newRouterCfg); err != nil {
			return err
		}

		routerSvc.UpdateConfig(newRouterCfg)
		return nil
	})

	if err := bot.Start(); err != nil {
		return nil, err
	}

	return bot, nil
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t
	return &tt
}

func minTimePtr(current *time.Time, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.Before(*current) {
		c := *candidate
		return &c
	}
	return current
}

func disableUntilSettingKey(accountID string) string {
	return accountDisableUntilKeyPrefix + accountID
}

func setDisableUntil(settings store.SettingsStore, accountID string, until time.Time) {
	if settings == nil || accountID == "" {
		return
	}
	_ = settings.Set(disableUntilSettingKey(accountID), strconv.FormatInt(until.Unix(), 10))
}

func clearDisableUntil(settings store.SettingsStore, accountID string) {
	if settings == nil || accountID == "" {
		return
	}
	_ = settings.Delete(disableUntilSettingKey(accountID))
}

func getDisableUntil(settings store.SettingsStore, accountID string) *time.Time {
	if settings == nil || accountID == "" {
		return nil
	}
	raw, ok := settings.Get(disableUntilSettingKey(accountID))
	if !ok || raw == "" {
		return nil
	}
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	ts := time.Unix(sec, 0)
	return &ts
}

func reconcileTemporaryDisables(s store.Store, settings store.SettingsStore) {
	if s == nil || settings == nil {
		return
	}
	now := time.Now()
	for _, acc := range s.ListAccounts() {
		if acc == nil {
			continue
		}
		until := getDisableUntil(settings, acc.ID)
		if until == nil {
			continue
		}
		if now.Before(*until) {
			continue
		}
		if !acc.Enabled {
			acc.Enabled = true
			acc.BlockedUntil = nil
			acc.UpdatedAt = now
			s.SetAccount(acc)
			_ = s.SetAccountBlockedUntil(acc.ID, nil)
		}
		clearDisableUntil(settings, acc.ID)
	}
}

type providerOAuthSpec struct {
	Provider     models.Provider
	ProviderType string
	ClientID     string
	ClientSecret string
	Scopes       []string
	Priority     int
}

type googleOAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	TokenType    string
	ExpiryDateMs int64
}

func oauthProviderSpec(provider string) (providerOAuthSpec, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "antigravity":
		clientID := firstNonEmptyEnv("QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID", "QUOTAGUARD_GOOGLE_CLIENT_ID")
		clientSecret := firstNonEmptyEnv("QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET", "QUOTAGUARD_GOOGLE_CLIENT_SECRET")
		if clientID == "" || clientSecret == "" {
			return providerOAuthSpec{}, fmt.Errorf("missing Antigravity OAuth client credentials in env (QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID/SECRET or QUOTAGUARD_GOOGLE_CLIENT_ID/SECRET)")
		}
		return providerOAuthSpec{
			Provider:     models.ProviderAnthropic,
			ProviderType: "antigravity",
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       antigravityOAuthScopes,
			Priority:     90,
		}, nil
	case "gemini":
		clientID := firstNonEmptyEnv("QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID", "QUOTAGUARD_GOOGLE_CLIENT_ID")
		clientSecret := firstNonEmptyEnv("QUOTAGUARD_GEMINI_OAUTH_CLIENT_SECRET", "QUOTAGUARD_GOOGLE_CLIENT_SECRET")
		if clientID == "" || clientSecret == "" {
			return providerOAuthSpec{}, fmt.Errorf("missing Gemini OAuth client credentials in env (QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID/SECRET or QUOTAGUARD_GOOGLE_CLIENT_ID/SECRET)")
		}
		return providerOAuthSpec{
			Provider:     models.ProviderGemini,
			ProviderType: "gemini",
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       geminiOAuthScopes,
			Priority:     70,
		}, nil
	default:
		return providerOAuthSpec{}, fmt.Errorf("unsupported provider: %s", provider)
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return val
		}
	}
	return ""
}

func newOAuthState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func oauthRedirectURI() string {
	redirectURI := strings.TrimSpace(os.Getenv("QUOTAGUARD_OAUTH_REDIRECT_URI"))
	if redirectURI == "" {
		return defaultOAuthCallbackURI
	}
	return redirectURI
}

func exchangeGoogleOAuthCode(ctx context.Context, spec providerOAuthSpec, code string) (googleOAuthToken, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return googleOAuthToken{}, fmt.Errorf("oauth code is empty")
	}

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", spec.ClientID)
	form.Set("client_secret", spec.ClientSecret)
	form.Set("redirect_uri", oauthRedirectURI())
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return googleOAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return googleOAuthToken{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return googleOAuthToken{}, fmt.Errorf("token exchange failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return googleOAuthToken{}, err
	}
	if payload.AccessToken == "" {
		return googleOAuthToken{}, fmt.Errorf("token exchange returned empty access_token")
	}

	expiry := int64(0)
	if payload.ExpiresIn > 0 {
		expiry = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UnixMilli()
	}
	return googleOAuthToken{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresIn:    payload.ExpiresIn,
		TokenType:    payload.TokenType,
		ExpiryDateMs: expiry,
	}, nil
}

func fetchGoogleUserEmail(ctx context.Context, accessToken string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", fmt.Errorf("access token is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleOAuthUserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("userinfo failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	email := strings.ToLower(strings.TrimSpace(payload.Email))
	if email == "" {
		return "", fmt.Errorf("userinfo returned empty email")
	}
	return email, nil
}

func findOrBuildAccountID(s store.Store, providerType, email string) (string, string) {
	candidate := sanitizeLocalAccountID(providerType + "_" + email)
	if s == nil {
		return candidate, ""
	}
	lowerEmail := strings.ToLower(strings.TrimSpace(email))
	for _, acc := range s.ListAccounts() {
		if acc == nil {
			continue
		}
		if strings.ToLower(acc.ProviderType) != strings.ToLower(providerType) {
			continue
		}
		creds, ok := s.GetAccountCredentials(acc.ID)
		if !ok || creds == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(creds.Email), lowerEmail) {
			return candidate, acc.ID
		}
	}
	if _, ok := s.GetAccount(candidate); ok {
		return candidate, candidate
	}
	return candidate, ""
}

func buildProviderAuthPath(accountManager *cliproxy.AccountManager, providerType, email string) string {
	baseDir := ""
	if accountManager != nil {
		baseDir = strings.TrimSpace(accountManager.GetAuthPath())
	}
	if baseDir == "" {
		baseDir = cliproxy.ResolveAuthPath("")
	}
	if baseDir == "" {
		baseDir = "/opt/cliproxyplus/auths"
	}
	_ = os.MkdirAll(baseDir, 0o755)
	name := fmt.Sprintf("%s-%s.json", providerType, sanitizeLocalFilename(email))
	return filepath.Join(baseDir, name)
}

func persistProviderAuthFile(path, providerType, email string, token googleOAuthToken, spec providerOAuthSpec) error {
	if path == "" {
		return fmt.Errorf("auth path is empty")
	}
	expiredAt := time.Now()
	if token.ExpiresIn > 0 {
		expiredAt = expiredAt.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	payload := map[string]interface{}{
		"type":          providerType,
		"email":         email,
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_uri":     googleOAuthTokenURL,
		"client_id":     spec.ClientID,
		"client_secret": spec.ClientSecret,
		"timestamp":     time.Now().UnixMilli(),
		"expires_in":    token.ExpiresIn,
		"expiry_date":   token.ExpiryDateMs,
		"expired":       expiredAt.Format(time.RFC3339Nano),
		"auth_method":   "telegram_oauth",
		"token": map[string]interface{}{
			"access_token":  token.AccessToken,
			"refresh_token": token.RefreshToken,
			"token_uri":     googleOAuthTokenURL,
			"client_id":     spec.ClientID,
			"client_secret": spec.ClientSecret,
			"expires_in":    token.ExpiresIn,
			"expiry":        expiredAt.Format(time.RFC3339Nano),
			"token_type":    token.TokenType,
			"scopes":        spec.Scopes,
		},
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func sanitizeLocalFilename(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	replacer := strings.NewReplacer(
		"@", "_at_",
		".", "_",
		"+", "_plus_",
		"/", "_",
		"\\", "_",
		":", "_",
		" ", "_",
	)
	s = replacer.Replace(s)
	if s == "" {
		return "account"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func sanitizeLocalAccountID(input string) string {
	s := sanitizeLocalFilename(input)
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}
