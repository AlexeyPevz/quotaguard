package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/api"
	"github.com/quotaguard/quotaguard/internal/cliproxy"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/quotaguard/quotaguard/internal/telegram"
)

const accountDisableUntilKeyPrefix = "router.account_disable_until."

const (
	googleOAuthAuthURL      = "https://accounts.google.com/o/oauth2/auth"
	googleOAuthTokenURL     = "https://oauth2.googleapis.com/token"
	googleOAuthUserInfoURL  = "https://www.googleapis.com/oauth2/v2/userinfo"
	defaultOAuthCallbackURI = "http://localhost:1456/oauth-callback"
	oauthRelayCallbackPath  = "/oauth/callback"
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
	ansiColorRE       = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	genericURLRE      = regexp.MustCompile(`https://[^\s]+`)
	sshTunnelCmdRE    = regexp.MustCompile(`ssh -L [^\n]+`)
	codexURLRE        = regexp.MustCompile(`https://auth\.openai\.com/[^\s]+`)
	codexDeviceCodeRE = regexp.MustCompile(`[A-Z0-9]{4,}-[A-Z0-9]{4,}`)
)

type deviceAuthSession struct {
	Provider         string
	LogPath          string
	CreatedAt        time.Time
	Cancel           context.CancelFunc
	LocalCallbackURL string

	mu        sync.RWMutex
	completed bool
	waitErr   error
}

func (s *deviceAuthSession) setResult(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed = true
	s.waitErr = err
}

func (s *deviceAuthSession) result() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.completed, s.waitErr
}

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
			creds, ok := s.GetAccountCredentials(acc.ID)
			if shouldHideTelegramAccount(acc, creds, ok) {
				delete(enabled, acc.ID)
				delete(providerType, acc.ID)
				delete(hasCreds, acc.ID)
				continue
			}
			if ok && creds != nil && creds.Email != "" {
				emails[acc.ID] = creds.Email
			}
		}

		quotas := s.ListQuotas()
		result := make([]telegram.AccountQuota, 0, len(quotas))
		seenQuota := make(map[string]struct{}, len(quotas))
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
			seenQuota[quota.AccountID] = struct{}{}
		}

		// Show newly connected accounts in Telegram even before first quota poll.
		for accountID, isEnabled := range enabled {
			if !isEnabled {
				continue
			}
			if _, ok := seenQuota[accountID]; ok {
				continue
			}
			if ok := hasCreds[accountID]; !ok {
				continue
			}
			providerLabel := providerType[accountID]
			if providerLabel == "" {
				providerLabel = "unknown"
			}
			result = append(result, telegram.AccountQuota{
				AccountID:    accountID,
				Provider:     providerLabel,
				Email:        emails[accountID],
				UsagePercent: 100,
				IsWarning:    false,
				Breakdown:    nil,
				IsActive:     accountID == activeAccountID,
				ResetAt:      nil,
				LastCallAt:   nil,
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
			creds, credsOk := s.GetAccountCredentials(acc.ID)
			if shouldHideTelegramAccount(acc, creds, credsOk) {
				continue
			}
			row := telegram.AccountControl{
				AccountID: acc.ID,
				Provider:  acc.ProviderType,
				Enabled:   acc.Enabled,
				IsActive:  acc.ID == activeAccountID,
			}
			if credsOk && creds != nil && creds.Email != "" {
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

	var (
		deviceAuthMu       sync.Mutex
		deviceAuthSessions = make(map[string]*deviceAuthSession)
	)

	api.SetOAuthCallbackHandler(func(c *gin.Context) {
		sid := strings.TrimSpace(c.Query("sid"))
		if sid == "" {
			c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Missing session", "Login session id is missing. Start login again from Telegram.")))
			return
		}

		deviceAuthMu.Lock()
		session, ok := deviceAuthSessions[sid]
		deviceAuthMu.Unlock()
		if !ok || session == nil {
			c.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Session not found", "This login session is no longer active. Start login again from Telegram.")))
			return
		}

		expectedProvider := normalizeLoginProvider(session.Provider)
		requestedProvider := normalizeLoginProvider(c.Param("provider"))
		if requestedProvider != "" && expectedProvider != "" && requestedProvider != expectedProvider {
			c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Provider mismatch", "Callback provider does not match the active session.")))
			return
		}

		forwardURL, err := buildLocalOAuthCallbackForwardURL(session.LocalCallbackURL, c.Request.URL.Query())
		if err != nil {
			c.Data(http.StatusBadRequest, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Callback error", err.Error())))
			return
		}

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, forwardURL, nil)
		if err != nil {
			c.Data(http.StatusInternalServerError, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Callback error", "Failed to prepare callback forwarding request.")))
			return
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Callback error", "Failed to reach local auth callback endpoint.")))
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			c.Data(http.StatusBadGateway, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Callback error", fmt.Sprintf("Local callback endpoint returned status %d.", resp.StatusCode))))
			return
		}

		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderOAuthRelayPage("Authorization complete", "You can return to Telegram. QuotaGuard will connect the account automatically.")))
	})

	finalizeAsync := func(state string, chatID int64, provider string, session *deviceAuthSession) {
		go func() {
			started := time.Now()
			for {
				completed, _ := session.result()
				if completed {
					break
				}
				if time.Since(started) > 16*time.Minute {
					if session.Cancel != nil {
						session.Cancel()
					}
					deviceAuthMu.Lock()
					if current, ok := deviceAuthSessions[state]; ok && current == session {
						delete(deviceAuthSessions, state)
					}
					deviceAuthMu.Unlock()
					telegram.Notify(cfg.Telegram.BotToken, chatID, fmt.Sprintf("❌ Login timeout for `%s`.", provider))
					return
				}
				time.Sleep(1 * time.Second)
			}

			var (
				result *telegram.LoginResult
				err    error
			)
			switch normalizeLoginProvider(provider) {
			case "codex":
				result, err = completeCodexDeviceAuthLogin(s, accountManager, session, "auto")
			default:
				result, err = completeCLIProxyProviderLogin(s, accountManager, session, normalizeLoginProvider(provider))
			}

			deviceAuthMu.Lock()
			if current, ok := deviceAuthSessions[state]; ok && current == session {
				delete(deviceAuthSessions, state)
			}
			deviceAuthMu.Unlock()

			if err != nil {
				telegram.Notify(cfg.Telegram.BotToken, chatID, fmt.Sprintf("❌ Login failed for `%s`: %v", provider, err))
				return
			}
			if result == nil {
				telegram.Notify(cfg.Telegram.BotToken, chatID, fmt.Sprintf("✅ `%s` connected.", provider))
				return
			}
			telegram.Notify(
				cfg.Telegram.BotToken,
				chatID,
				fmt.Sprintf(
					"✅ Account connected\nProvider: `%s`\nEmail: `%s`\nAccount: `%s`",
					result.Provider,
					result.Email,
					result.AccountID,
				),
			)
		}()
	}

	bot.SetLoginCallbacks(
		func(provider string, chatID int64) (*telegram.LoginURLPayload, error) {
			switch normalizeLoginProvider(provider) {
			case "codex":
				state, err := newOAuthState()
				if err != nil {
					return nil, err
				}
				session, authURL, oneTimeCode, err := startCodexDeviceAuth(state)
				if err != nil {
					return nil, err
				}
				deviceAuthMu.Lock()
				deviceAuthSessions[state] = session
				deviceAuthMu.Unlock()
				finalizeAsync(state, chatID, provider, session)

				return &telegram.LoginURLPayload{
					Provider:     provider,
					State:        state,
					Mode:         "device",
					URL:          authURL,
					Instructions: fmt.Sprintf("Открой ссылку и введи одноразовый код: %s. После успешного входа аккаунт подключится автоматически.", oneTimeCode),
				}, nil
			case "claude":
				state, err := newOAuthState()
				if err != nil {
					return nil, err
				}
				session, authURL, instructions, err := startCLIProxyProviderLogin(state, "claude")
				if err != nil {
					return nil, err
				}
				deviceAuthMu.Lock()
				deviceAuthSessions[state] = session
				deviceAuthMu.Unlock()
				finalizeAsync(state, chatID, provider, session)
				return &telegram.LoginURLPayload{
					Provider:     provider,
					State:        state,
					Mode:         "device",
					URL:          authURL,
					Instructions: instructions + " После успешного callback аккаунт подключится автоматически.",
				}, nil
			case "antigravity", "gemini", "qwen":
				state, err := newOAuthState()
				if err != nil {
					return nil, err
				}
				session, authURL, instructions, err := startCLIProxyProviderLogin(state, normalizeLoginProvider(provider))
				if err == nil {
					deviceAuthMu.Lock()
					deviceAuthSessions[state] = session
					deviceAuthMu.Unlock()
					finalizeAsync(state, chatID, provider, session)
					return &telegram.LoginURLPayload{
						Provider:     provider,
						State:        state,
						Mode:         "device",
						URL:          authURL,
						Instructions: instructions + " После успешного callback аккаунт подключится автоматически.",
					}, nil
				}
			}

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
				Mode:     "oauth",
				URL:      googleOAuthAuthURL + "?" + values.Encode(),
				Instructions: "Откроется Google OAuth. После редиректа скопируй URL из адресной строки " +
					"и отправь его сюда.",
			}, nil
		},
		func(provider, state string, code string, _ int64) (*telegram.LoginResult, error) {
			switch normalizeLoginProvider(provider) {
			case "codex":
				deviceAuthMu.Lock()
				session, ok := deviceAuthSessions[state]
				deviceAuthMu.Unlock()
				if !ok || session == nil {
					return nil, fmt.Errorf("codex auth session not found, restart login from the menu")
				}
				action := strings.ToLower(strings.TrimSpace(code))
				if action != "done" && action != "check" && action != "ok" && action != "готово" {
					return nil, fmt.Errorf("after browser login send `done`")
				}
				completed, _ := session.result()
				if !completed {
					return nil, fmt.Errorf("authorization still in progress, finish browser login and send `done` again")
				}
				deviceAuthMu.Lock()
				delete(deviceAuthSessions, state)
				deviceAuthMu.Unlock()
				return completeCodexDeviceAuthLogin(s, accountManager, session, code)
			case "claude":
				deviceAuthMu.Lock()
				session, ok := deviceAuthSessions[state]
				deviceAuthMu.Unlock()
				if !ok || session == nil {
					return nil, fmt.Errorf("claude auth session not found, restart login from the menu")
				}
				action := strings.ToLower(strings.TrimSpace(code))
				if action != "done" && action != "check" && action != "ok" && action != "готово" {
					return nil, fmt.Errorf("after browser login send `done`")
				}
				completed, _ := session.result()
				if !completed {
					return nil, fmt.Errorf("authorization still in progress, finish browser login and send `done` again")
				}
				deviceAuthMu.Lock()
				delete(deviceAuthSessions, state)
				deviceAuthMu.Unlock()
				return completeCLIProxyProviderLogin(s, accountManager, session, "claude")
			case "antigravity", "gemini", "qwen":
				deviceAuthMu.Lock()
				session, ok := deviceAuthSessions[state]
				deviceAuthMu.Unlock()
				if ok && session != nil {
					action := strings.ToLower(strings.TrimSpace(code))
					if action != "done" && action != "check" && action != "ok" && action != "готово" {
						return nil, fmt.Errorf("after browser login send `done`")
					}
					completed, _ := session.result()
					if !completed {
						return nil, fmt.Errorf("authorization still in progress, finish browser login and send `done` again")
					}
					deviceAuthMu.Lock()
					delete(deviceAuthSessions, state)
					deviceAuthMu.Unlock()
					return completeCLIProxyProviderLogin(s, accountManager, session, normalizeLoginProvider(provider))
				}
			}

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
			clientID, clientSecret = fallbackGoogleOAuthClientCreds("antigravity")
		}
		if clientID == "" || clientSecret == "" {
			return providerOAuthSpec{}, fmt.Errorf("missing Antigravity OAuth client credentials: set QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID/SECRET or QUOTAGUARD_GOOGLE_CLIENT_ID/SECRET, or place a valid Google OAuth JSON in auth paths")
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
			clientID, clientSecret = fallbackGoogleOAuthClientCreds("gemini")
		}
		if clientID == "" || clientSecret == "" {
			return providerOAuthSpec{}, fmt.Errorf("missing Gemini OAuth client credentials: set QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID/SECRET or QUOTAGUARD_GOOGLE_CLIENT_ID/SECRET, or place a valid Google OAuth JSON in auth paths")
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

func normalizeLoginProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex", "openai":
		return "codex"
	case "claude", "claude-code", "claude_code":
		return "claude"
	case "antigravity", "cloudcode":
		return "antigravity"
	case "gemini":
		return "gemini"
	case "qwen", "dashscope":
		return "qwen"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func startCodexDeviceAuth(state string) (*deviceAuthSession, string, string, error) {
	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("qg-codex-device-%s.log", state))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	cmd := exec.CommandContext(ctx, "codex", "login", "--device-auth")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb")

	if err := cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return nil, "", "", err
	}

	session := &deviceAuthSession{
		Provider:  "codex",
		LogPath:   logPath,
		CreatedAt: time.Now(),
		Cancel:    cancel,
	}

	go func() {
		waitErr := cmd.Wait()
		session.setResult(waitErr)
		_ = logFile.Close()
	}()

	deadline := time.Now().Add(12 * time.Second)
	var authURL, oneTimeCode string
	for time.Now().Before(deadline) {
		body, _ := os.ReadFile(logPath)
		clean := stripANSI(body)
		if authURL == "" {
			if match := codexURLRE.Find(clean); len(match) > 0 {
				authURL = string(match)
			}
		}
		if oneTimeCode == "" {
			if match := codexDeviceCodeRE.Find(clean); len(match) > 0 {
				oneTimeCode = string(match)
			}
		}
		if authURL != "" && oneTimeCode != "" {
			return session, authURL, oneTimeCode, nil
		}

		completed, waitErr := session.result()
		if completed && waitErr != nil {
			if session.Cancel != nil {
				session.Cancel()
			}
			return nil, "", "", fmt.Errorf("codex login failed to start: %v", waitErr)
		}
		time.Sleep(200 * time.Millisecond)
	}

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if session.Cancel != nil {
		session.Cancel()
	}
	return nil, "", "", fmt.Errorf("failed to initialize codex device auth, no URL/code emitted")
}

func startCLIProxyProviderLogin(state, provider string) (*deviceAuthSession, string, string, error) {
	binPath := strings.TrimSpace(firstNonEmptyEnv("QUOTAGUARD_CLIPROXY_BIN_PATH"))
	if binPath == "" {
		binPath = "/opt/cliproxyplus/cli-proxy-api-plus"
	}
	if _, err := os.Stat(binPath); err != nil {
		return nil, "", "", fmt.Errorf("cliproxy binary not found: %s", binPath)
	}

	flag := ""
	switch provider {
	case "antigravity":
		flag = "-antigravity-login"
	case "gemini":
		flag = "-login"
	case "claude":
		flag = "-claude-login"
	case "qwen":
		flag = "-qwen-login"
	default:
		return nil, "", "", fmt.Errorf("unsupported cliproxy provider: %s", provider)
	}

	args := []string{}
	configPath := strings.TrimSpace(firstNonEmptyEnv("QUOTAGUARD_CLIPROXY_CONFIG_PATH"))
	if configPath == "" {
		if _, err := os.Stat("/opt/cliproxyplus/config.yaml"); err == nil {
			configPath = "/opt/cliproxyplus/config.yaml"
		}
	}
	if configPath != "" {
		args = append(args, "-config", configPath)
	}
	args = append(args, flag, "-no-browser")

	logPath := filepath.Join(os.TempDir(), fmt.Sprintf("qg-cliproxy-%s-%s.log", provider, state))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb")
	if provider == "qwen" {
		alias := strings.TrimSpace(os.Getenv("QUOTAGUARD_QWEN_LOGIN_ALIAS"))
		if alias == "" {
			shortState := strings.TrimSpace(state)
			if len(shortState) > 8 {
				shortState = shortState[:8]
			}
			if shortState == "" {
				shortState = strconv.FormatInt(time.Now().Unix(), 36)
			}
			alias = "qwen_" + shortState
		}
		cmd.Stdin = strings.NewReader(alias + "\n")
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return nil, "", "", err
	}

	session := &deviceAuthSession{
		Provider:  provider,
		LogPath:   logPath,
		CreatedAt: time.Now(),
		Cancel:    cancel,
	}
	go func() {
		waitErr := cmd.Wait()
		session.setResult(waitErr)
		_ = logFile.Close()
	}()

	deadline := time.Now().Add(12 * time.Second)
	var authURL, tunnelCmd string
	for time.Now().Before(deadline) {
		body, _ := os.ReadFile(logPath)
		clean := stripANSI(body)
		if authURL == "" {
			if match := genericURLRE.Find(clean); len(match) > 0 {
				authURL = string(match)
			}
		}
		if tunnelCmd == "" {
			if match := sshTunnelCmdRE.Find(clean); len(match) > 0 {
				tunnelCmd = strings.TrimSpace(string(match))
			}
		}
		if authURL != "" {
			rewrittenURL, localCallbackURL, relayEnabled, rewriteErr := rewriteProviderAuthURLForRelay(authURL, provider, state)
			if rewriteErr == nil && rewrittenURL != "" {
				authURL = rewrittenURL
			}
			session.LocalCallbackURL = localCallbackURL

			instructions := "Открой URL для авторизации."
			if relayEnabled {
				instructions = "Открой URL для авторизации. После входа браузер покажет успешный callback и аккаунт подключится автоматически."
			} else if tunnelCmd != "" {
				instructions = fmt.Sprintf("На локальной машине открой SSH-туннель:\n`%s`\n\nЗатем открой URL для авторизации.", tunnelCmd)
			}
			if rewriteErr != nil {
				instructions += fmt.Sprintf("\n\nPublic callback relay не активирован: %v", rewriteErr)
			}
			return session, authURL, instructions, nil
		}
		completed, waitErr := session.result()
		if completed && waitErr != nil {
			if session.Cancel != nil {
				session.Cancel()
			}
			return nil, "", "", fmt.Errorf("cliproxy %s login failed to start: %v", provider, waitErr)
		}
		time.Sleep(200 * time.Millisecond)
	}

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if session.Cancel != nil {
		session.Cancel()
	}
	return nil, "", "", fmt.Errorf("failed to initialize %s login, no URL emitted", provider)
}

func completeCodexDeviceAuthLogin(
	s store.Store,
	accountManager *cliproxy.AccountManager,
	session *deviceAuthSession,
	_ string,
) (*telegram.LoginResult, error) {
	if session == nil {
		return nil, fmt.Errorf("codex auth session is nil")
	}

	completed, waitErr := session.result()
	if !completed {
		return nil, fmt.Errorf("authorization still in progress")
	}
	if waitErr != nil {
		return nil, fmt.Errorf("codex login failed: %v", waitErr)
	}
	if session.Cancel != nil {
		session.Cancel()
	}

	authPath := filepath.Join(os.Getenv("HOME"), ".codex", "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		return nil, fmt.Errorf("codex auth file not found after login: %w", err)
	}

	var parsed struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			IDToken      string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	accessToken := strings.TrimSpace(parsed.Tokens.AccessToken)
	providerAccountID := strings.TrimSpace(parsed.Tokens.AccountID)
	if accessToken == "" || providerAccountID == "" {
		return nil, fmt.Errorf("codex auth file missing access_token/account_id")
	}
	refreshToken := strings.TrimSpace(parsed.Tokens.RefreshToken)
	email := extractEmailFromJWT(parsed.Tokens.IDToken)
	if email == "" {
		email = "codex@local"
	}

	accountID, existingID := findOrBuildAccountID(s, "codex", email)
	if existingID != "" {
		accountID = existingID
	}
	authRef := buildProviderAuthPath(accountManager, "codex", email)
	now := time.Now()
	account := &models.Account{
		ID:             accountID,
		Provider:       models.ProviderOpenAI,
		ProviderType:   "codex",
		Enabled:        true,
		Priority:       80,
		CredentialsRef: authRef,
		OAuthCredsPath: authRef,
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
		"type":                "codex",
		"email":               email,
		"access_token":        accessToken,
		"refresh_token":       refreshToken,
		"provider_account_id": providerAccountID,
		"timestamp":           now.UnixMilli(),
		"auth_method":         "codex_device_auth",
	}
	rawJSON, _ := json.Marshal(rawPayload)
	creds := &models.AccountCredentials{
		Type:              "codex",
		Email:             email,
		AccessToken:       accessToken,
		RefreshToken:      refreshToken,
		ProviderAccountID: providerAccountID,
		SourcePath:        authRef,
		Raw:               string(rawJSON),
	}
	if err := s.SetAccountCredentials(accountID, creds); err != nil {
		return nil, err
	}
	if err := persistCodexAuthFile(authRef, email, accessToken, refreshToken, providerAccountID); err != nil {
		return nil, err
	}

	return &telegram.LoginResult{
		AccountID: accountID,
		Email:     email,
		Provider:  "codex",
	}, nil
}

func completeCLIProxyProviderLogin(
	s store.Store,
	accountManager *cliproxy.AccountManager,
	session *deviceAuthSession,
	providerType string,
) (*telegram.LoginResult, error) {
	if session == nil {
		return nil, fmt.Errorf("auth session is nil")
	}
	completed, waitErr := session.result()
	if !completed {
		return nil, fmt.Errorf("authorization still in progress")
	}
	if waitErr != nil {
		return nil, fmt.Errorf("%s login failed: %v", providerType, waitErr)
	}
	if session.Cancel != nil {
		session.Cancel()
	}

	providerAliases := map[string]struct{}{providerType: {}}
	switch providerType {
	case "qwen":
		providerAliases["dashscope"] = struct{}{}
		providerAliases["qwen_oauth"] = struct{}{}
	case "gemini":
		providerAliases["gemini_oauth"] = struct{}{}
	}

	before := make(map[string]struct{})
	for _, acc := range s.ListAccounts() {
		if acc == nil {
			continue
		}
		if _, ok := providerAliases[strings.ToLower(strings.TrimSpace(acc.ProviderType))]; ok {
			before[acc.ID] = struct{}{}
		}
	}

	if accountManager != nil {
		if _, _, err := accountManager.ScanAndSync(); err != nil {
			return nil, err
		}
	}
	_, _, _ = importOAuthCredentials(s)

	var selected *models.Account
	for _, acc := range s.ListAccounts() {
		if acc == nil {
			continue
		}
		if _, ok := providerAliases[strings.ToLower(strings.TrimSpace(acc.ProviderType))]; ok {
			if _, existed := before[acc.ID]; !existed {
				selected = acc
				break
			}
			if selected == nil {
				selected = acc
			}
		}
	}
	if selected == nil {
		return &telegram.LoginResult{
			AccountID: "updated",
			Email:     "unknown",
			Provider:  providerType,
		}, nil
	}
	email := "unknown"
	if creds, ok := s.GetAccountCredentials(selected.ID); ok && creds != nil && strings.TrimSpace(creds.Email) != "" {
		email = strings.TrimSpace(creds.Email)
	}
	return &telegram.LoginResult{
		AccountID: selected.ID,
		Email:     email,
		Provider:  providerType,
	}, nil
}

func rewriteProviderAuthURLForRelay(authURL, provider, sid string) (string, string, bool, error) {
	parsedAuthURL, err := url.Parse(strings.TrimSpace(authURL))
	if err != nil || parsedAuthURL == nil || parsedAuthURL.Scheme == "" || parsedAuthURL.Host == "" {
		return authURL, "", false, fmt.Errorf("invalid provider auth URL")
	}
	query := parsedAuthURL.Query()

	localCallbackURL := strings.TrimSpace(query.Get("redirect_uri"))
	if localCallbackURL == "" {
		return parsedAuthURL.String(), "", false, nil
	}
	parsedLocalCallback, err := url.Parse(localCallbackURL)
	if err != nil || parsedLocalCallback == nil || parsedLocalCallback.Scheme == "" || parsedLocalCallback.Host == "" {
		return parsedAuthURL.String(), "", false, fmt.Errorf("invalid redirect_uri in provider auth URL")
	}

	publicBaseURL := strings.TrimSpace(os.Getenv("QUOTAGUARD_PUBLIC_BASE_URL"))
	if publicBaseURL == "" {
		return parsedAuthURL.String(), parsedLocalCallback.String(), false, nil
	}

	publicCallbackURL, err := buildPublicRelayCallbackURL(publicBaseURL, provider, sid)
	if err != nil {
		return parsedAuthURL.String(), parsedLocalCallback.String(), false, err
	}

	query.Set("redirect_uri", publicCallbackURL)
	parsedAuthURL.RawQuery = query.Encode()

	return parsedAuthURL.String(), parsedLocalCallback.String(), true, nil
}

func buildPublicRelayCallbackURL(baseURL, provider, sid string) (string, error) {
	parsedBase, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsedBase == nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return "", fmt.Errorf("QUOTAGUARD_PUBLIC_BASE_URL must be an absolute URL")
	}

	provider = normalizeLoginProvider(provider)
	if provider == "" {
		provider = "oauth"
	}

	basePath := strings.TrimRight(parsedBase.Path, "/")
	parsedBase.Path = fmt.Sprintf("%s%s/%s", basePath, oauthRelayCallbackPath, provider)

	query := parsedBase.Query()
	query.Set("sid", strings.TrimSpace(sid))
	parsedBase.RawQuery = query.Encode()
	return parsedBase.String(), nil
}

func buildLocalOAuthCallbackForwardURL(localCallbackURL string, incoming url.Values) (string, error) {
	localCallbackURL = strings.TrimSpace(localCallbackURL)
	if localCallbackURL == "" {
		return "", fmt.Errorf("local callback URL is empty for this auth session")
	}
	target, err := url.Parse(localCallbackURL)
	if err != nil || target == nil || target.Scheme == "" || target.Host == "" {
		return "", fmt.Errorf("local callback URL is invalid")
	}

	query := target.Query()
	for key, values := range incoming {
		if strings.EqualFold(key, "sid") {
			continue
		}
		for _, value := range values {
			query.Add(key, value)
		}
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func renderOAuthRelayPage(title, body string) string {
	return fmt.Sprintf(
		"<!doctype html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><h2>%s</h2><p>%s</p></body></html>",
		html.EscapeString(strings.TrimSpace(title)),
		html.EscapeString(strings.TrimSpace(title)),
		html.EscapeString(strings.TrimSpace(body)),
	)
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return val
		}
	}
	return ""
}

func fallbackGoogleOAuthClientCreds(provider string) (string, string) {
	type candidate struct {
		path string
		raw  map[string]interface{}
	}

	readJSON := func(path string) (map[string]interface{}, bool) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, false
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, false
		}
		return parsed, true
	}

	extractCreds := func(raw map[string]interface{}) (string, string) {
		clientID := strings.TrimSpace(readString(raw["client_id"]))
		clientSecret := strings.TrimSpace(readString(raw["client_secret"]))
		if clientID != "" && clientSecret != "" {
			return clientID, clientSecret
		}
		if tokenMap, ok := raw["token"].(map[string]interface{}); ok {
			clientID = strings.TrimSpace(readString(tokenMap["client_id"]))
			clientSecret = strings.TrimSpace(readString(tokenMap["client_secret"]))
			if clientID != "" && clientSecret != "" {
				return clientID, clientSecret
			}
		}
		return "", ""
	}

	candidates := make([]candidate, 0, 32)

	authDir := strings.TrimSpace(cliproxy.ResolveAuthPath(""))
	if authDir != "" {
		if entries, err := os.ReadDir(authDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
					continue
				}
				path := filepath.Join(authDir, entry.Name())
				if raw, ok := readJSON(path); ok {
					candidates = append(candidates, candidate{path: path, raw: raw})
				}
			}
		}
	}

	for _, path := range []string{
		expandLocalHome("~/.gemini/oauth_creds.json"),
		expandLocalHome("~/.config/google-gemini/token.json"),
		expandLocalHome("~/.config/gcloud/application_default_credentials.json"),
	} {
		if raw, ok := readJSON(path); ok {
			candidates = append(candidates, candidate{path: path, raw: raw})
		}
	}

	match := func(path string, raw map[string]interface{}, target string) bool {
		p := strings.ToLower(path)
		t := strings.ToLower(strings.TrimSpace(readString(raw["type"])))
		switch target {
		case "antigravity":
			return strings.Contains(p, "antigravity") || t == "antigravity" || t == "cloudcode"
		case "gemini":
			return strings.Contains(p, "gemini") || t == "gemini" || strings.Contains(p, "gcloud")
		default:
			return false
		}
	}

	for _, c := range candidates {
		if !match(c.path, c.raw, provider) {
			continue
		}
		if id, secret := extractCreds(c.raw); id != "" && secret != "" {
			return id, secret
		}
	}
	for _, c := range candidates {
		if id, secret := extractCreds(c.raw); id != "" && secret != "" {
			return id, secret
		}
	}
	return "", ""
}

func expandLocalHome(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return path
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

func persistCodexAuthFile(path, email, accessToken, refreshToken, accountID string) error {
	if path == "" {
		return fmt.Errorf("auth path is empty")
	}
	payload := map[string]interface{}{
		"type":          "codex",
		"email":         email,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"account_id":    accountID,
		"timestamp":     time.Now().UnixMilli(),
		"auth_method":   "codex_device_auth",
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

func stripANSI(input []byte) []byte {
	if len(input) == 0 {
		return input
	}
	return bytes.TrimSpace(ansiColorRE.ReplaceAll(input, nil))
}

func extractEmailFromJWT(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(claims.Email))
}

func readString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
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

func shouldHideTelegramAccount(acc *models.Account, creds *models.AccountCredentials, hasCreds bool) bool {
	if acc == nil {
		return true
	}
	if strings.TrimSpace(acc.ProviderType) == "" || strings.TrimSpace(acc.CredentialsRef) == "" {
		return true
	}
	if !hasCreds || creds == nil {
		return false
	}
	email := strings.ToLower(strings.TrimSpace(creds.Email))
	ref := strings.ToLower(strings.TrimSpace(acc.CredentialsRef))
	if email == "manual@local" {
		return true
	}
	if strings.Contains(ref, "telegram-manual") {
		return true
	}
	return false
}
