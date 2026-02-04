package cli

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/quotaguard/quotaguard/internal/cliproxy"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/quotaguard/quotaguard/internal/telegram"
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

	var cfgMu sync.Mutex
	currentRouterCfg := routerCfg

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
		cfgMu.Lock()
		warnThreshold := currentRouterCfg.WarningThreshold
		cfgMu.Unlock()

		quotas := s.ListQuotas()
		result := make([]telegram.AccountQuota, 0, len(quotas))
		for _, quota := range quotas {
			if quota == nil {
				continue
			}
			remaining := quota.EffectiveRemainingWithVirtual()
			usage := 100.0 - remaining
			result = append(result, telegram.AccountQuota{
				AccountID:    quota.AccountID,
				Provider:     string(quota.Provider),
				UsagePercent: usage,
				IsWarning:    remaining < warnThreshold,
			})
		}
		return result, nil
	})

	bot.SetAlertsCallback(func() ([]telegram.ActiveAlert, error) {
		return []telegram.ActiveAlert{}, nil
	})

	bot.SetThresholdsCallback(func(warning, switchVal, critical float64) error {
		cfgMu.Lock()
		currentRouterCfg.WarningThreshold = warning
		currentRouterCfg.SwitchThreshold = switchVal
		currentRouterCfg.CriticalThreshold = critical
		newCfg := currentRouterCfg
		cfgMu.Unlock()

		routerSvc.UpdateConfig(newCfg)
		return nil
	})

	bot.SetPolicyCallback(func(policy string) error {
		cfgMu.Lock()
		currentRouterCfg.DefaultPolicy = policy
		newCfg := currentRouterCfg
		cfgMu.Unlock()

		routerSvc.UpdateConfig(newCfg)
		return nil
	})

	bot.SetFallbackCallback(func(chains map[string][]string) error {
		cfgMu.Lock()
		currentRouterCfg.FallbackChains = chains
		newCfg := currentRouterCfg
		cfgMu.Unlock()

		routerSvc.UpdateConfig(newCfg)
		return nil
	})

	bot.SetImportCallback(func(path string) (int, int, error) {
		if accountManager == nil {
			return 0, 0, fmt.Errorf("account manager not initialized")
		}
		if path != "" {
			accountManager = cliproxy.NewAccountManager(s, path, 5*time.Minute)
		}
		return accountManager.ScanAndSync()
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

		cfgMu.Lock()
		currentRouterCfg = newRouterCfg
		cfgMu.Unlock()
		routerSvc.UpdateConfig(newRouterCfg)
		return nil
	})

	if err := bot.Start(); err != nil {
		return nil, err
	}

	return bot, nil
}
