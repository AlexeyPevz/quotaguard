package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/quotaguard/quotaguard/internal/alerts"
	"github.com/quotaguard/quotaguard/internal/api"
	"github.com/quotaguard/quotaguard/internal/cliproxy"
	"github.com/quotaguard/quotaguard/internal/collector"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/reservation"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/quotaguard/quotaguard/internal/telegram"
	"github.com/spf13/cobra"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:     "serve",
	Aliases: []string{"s", "server", "run"},
	Short:   "Start the QuotaGuard server",
	Long: `Start the QuotaGuard server in main mode.

This command starts the HTTP server that handles quota management,
routing requests, and health monitoring.

Example:
  quotaguard serve --config config.yaml --db ./data/quotaguard.db

The server will start listening on the address configured in the config file.`,
	RunE: runServe,
}

var serveFlags struct {
	Host       string
	Port       int
	Timeout    time.Duration
	TLS        bool
	TLSCert    string
	TLSKey     string
	TLSVersion string
}

func init() {
	serveCmd.Flags().StringVar(&serveFlags.Host, "host", "", "Server host (overrides config)")
	serveCmd.Flags().IntVar(&serveFlags.Port, "port", 0, "Server port (overrides config)")
	serveCmd.Flags().DurationVar(&serveFlags.Timeout, "timeout", envDuration("SHUTDOWN_TIMEOUT", 30*time.Second), "Shutdown timeout")
	serveCmd.Flags().BoolVar(&serveFlags.TLS, "tls", false, "Enable TLS/HTTPS")
	serveCmd.Flags().StringVar(&serveFlags.TLSCert, "cert", "", "TLS certificate file path")
	serveCmd.Flags().StringVar(&serveFlags.TLSKey, "key", "", "TLS key file path")
	serveCmd.Flags().StringVar(&serveFlags.TLSVersion, "tls-version", "1.3", "Minimum TLS version (1.2 or 1.3)")

	RootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	if globalFlags.Verbose {
		log.Println("Starting QuotaGuard server...")
		log.Printf("Config path: %s", globalFlags.Config)
		log.Printf("Database path: %s", globalFlags.DBPath)
	}

	// Load configuration
	loader := config.NewLoader(globalFlags.Config)
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Apply CLI flags to config
	if serveFlags.Host != "" {
		cfg.Server.Host = serveFlags.Host
	}
	if serveFlags.Port != 0 {
		cfg.Server.HTTPPort = serveFlags.Port
	}
	if serveFlags.TLS {
		cfg.Server.TLS.Enabled = true
	}
	if serveFlags.TLSCert != "" {
		cfg.Server.TLS.CertFile = serveFlags.TLSCert
	}
	if serveFlags.TLSKey != "" {
		cfg.Server.TLS.KeyFile = serveFlags.TLSKey
	}
	if serveFlags.TLSVersion != "" {
		cfg.Server.TLS.MinVersion = serveFlags.TLSVersion
	}

	if globalFlags.Verbose {
		log.Printf("Configuration loaded successfully")
		log.Printf("Server host: %s, port: %d", cfg.Server.Host, cfg.Server.HTTPPort)
		if cfg.Server.TLS.Enabled {
			log.Printf("TLS enabled: true, cert: %s, min_version: %s", cfg.Server.TLS.CertFile, cfg.Server.TLS.MinVersion)
		}
	}

	// Validate TLS configuration if enabled
	if cfg.Server.TLS.Enabled {
		if err := validateTLSConfig(cfg.Server.TLS); err != nil {
			return fmt.Errorf("TLS validation failed: %w", err)
		}
		if globalFlags.Verbose {
			log.Println("TLS configuration validated successfully")
		}
	}

	// Create SQLite store with WAL mode enabled
	sqliteStore, err := store.NewSQLiteStore(globalFlags.DBPath)
	if err != nil {
		return fmt.Errorf("failed to create SQLite store: %w", err)
	}

	settingsStore := sqliteStore.Settings()
	if err := ensureSettingsDefaults(settingsStore, cfg); err != nil {
		return fmt.Errorf("failed to seed settings defaults: %w", err)
	}
	applySettingsToTelegramConfig(settingsStore, &cfg.Telegram)

	if err := seedAccountsFromConfig(sqliteStore, cfg); err != nil {
		return fmt.Errorf("failed to seed accounts from config: %w", err)
	}

	var accountManager *cliproxy.AccountManager
	authsPath := cliproxy.ResolveAuthPath("")
	oauthNew, oauthUpdated, oauthErr := importOAuthCredentials(sqliteStore)
	if oauthErr != nil && globalFlags.Verbose {
		log.Printf("OAuth import warning: %v", oauthErr)
	}
	if authsPath != "" {
		accountManager = cliproxy.NewAccountManager(sqliteStore, authsPath, 5*time.Minute)
		newCount, updatedCount, err := accountManager.ScanAndSync()
		if err != nil {
			log.Printf("Auto-discovery warning: %v", err)
		} else {
			log.Printf("Auto-discovery enabled: %s (new=%d updated=%d, oauth_new=%d oauth_updated=%d)", authsPath, newCount, updatedCount, oauthNew, oauthUpdated)
			if cfg.Telegram.Enabled && settingsStore != nil {
				if chatID := settingsStore.GetInt(store.SettingTelegramChatID, 0); chatID != 0 {
					msg := fmt.Sprintf("🔄 Auto-import: %d new, %d updated (oauth: %d new, %d updated)", newCount, updatedCount, oauthNew, oauthUpdated)
					telegram.Notify(cfg.Telegram.BotToken, int64(chatID), msg)
				}
			}
		}
		if err := accountManager.StartAutoSync(context.Background()); err != nil {
			log.Printf("Auto-discovery warning: %v", err)
		}
	} else if oauthNew > 0 || oauthUpdated > 0 {
		log.Printf("OAuth import complete (oauth_new=%d oauth_updated=%d)", oauthNew, oauthUpdated)
		if cfg.Telegram.Enabled && settingsStore != nil {
			if chatID := settingsStore.GetInt(store.SettingTelegramChatID, 0); chatID != 0 {
				msg := fmt.Sprintf("🔄 Auto-import: 0 new, 0 updated (oauth: %d new, %d updated)", oauthNew, oauthUpdated)
				telegram.Notify(cfg.Telegram.BotToken, int64(chatID), msg)
			}
		}
	}

	if globalFlags.Verbose {
		log.Printf("Database initialized at: %s", globalFlags.DBPath)
	}

	defer func() {
		if globalFlags.Verbose {
			log.Println("Shutting down gracefully...")
		}
		if err := sqliteStore.Close(); err != nil {
			log.Printf("Error closing store: %v", err)
		}
		if globalFlags.Verbose {
			log.Println("Store closed successfully")
		}
	}()

	// Create router service with converted config
	routerConfig := router.Config{
		WarningThreshold:    cfg.Router.Thresholds.Warning,
		SwitchThreshold:     cfg.Router.Thresholds.Switch,
		CriticalThreshold:   cfg.Router.Thresholds.Critical,
		MinSafeThreshold:    cfg.Router.Thresholds.MinSafe,
		MinDwellTime:        cfg.Router.AntiFlapping.MinDwellTime,
		CooldownAfterSwitch: cfg.Router.AntiFlapping.CooldownAfterSwitch,
		HysteresisMargin:    cfg.Router.AntiFlapping.HysteresisMargin,
		IgnoreEstimated:     cfg.Router.IgnoreEstimated,
		Weights: router.Weights{
			Safety:      cfg.Router.Weights.Safety,
			Refill:      cfg.Router.Weights.Refill,
			Tier:        cfg.Router.Weights.Tier,
			Reliability: cfg.Router.Weights.Reliability,
			Cost:        cfg.Router.Weights.Cost,
		},
		DefaultPolicy:  "balanced",
		Policies:       buildRouterPolicyMap(&cfg.Router),
		FallbackChains: cfg.Router.FallbackChains,
	}
	if err := applySettingsToRouterConfig(settingsStore, &routerConfig); err != nil {
		return fmt.Errorf("failed to apply settings to router config: %w", err)
	}
	cfg.Router.Thresholds.Warning = routerConfig.WarningThreshold
	cfg.Router.Thresholds.Switch = routerConfig.SwitchThreshold
	cfg.Router.Thresholds.Critical = routerConfig.CriticalThreshold
	cfg.Router.FallbackChains = routerConfig.FallbackChains
	routerSvc := router.NewRouter(sqliteStore, routerConfig)

	// Create reservation manager
	reservationConfig := reservation.Config{
		DefaultTTL: cfg.Router.Reservation.Timeout,
	}
	reservationMgr := reservation.NewManager(sqliteStore, reservationConfig)

	// Create passive collector
	passiveCollector := collector.NewPassiveCollector(
		sqliteStore,
		cfg.Collector.Passive.BufferSize,
		cfg.Collector.Passive.FlushInterval,
	)

	// Create active collector (if enabled)
	var activeCollector *collector.ActiveCollector
	var providerFetcher collector.QuotaFetcher
	if cfg.Collector.Mode == "active" || cfg.Collector.Mode == "hybrid" {
		fetcher := collector.NewProviderFetcher(sqliteStore)
		providerFetcher = fetcher
		activeCfg := collector.Config{
			Interval:      cfg.Collector.Active.DefaultInterval,
			Adaptive:      cfg.Collector.Active.Adaptive,
			Timeout:       cfg.Collector.Active.Timeout,
			RetryAttempts: cfg.Collector.Active.RetryAttempts,
			RetryBackoff:  time.Second,
			CBEnabled:     true,
			CBThreshold:   3,
			CBTimeout:     5 * time.Minute,
			WorkerCount:   envInt("QUOTAGUARD_COLLECTOR_WORKERS", 8),
			Jitter:        envDuration("QUOTAGUARD_COLLECTOR_JITTER", 250*time.Millisecond),
		}
		activeCollector = collector.NewActiveCollector(sqliteStore, fetcher, activeCfg, nil)
		if err := activeCollector.Start(context.Background()); err != nil {
			log.Printf("Active collector warning: %v", err)
		}
	}

	// Create API server
	server := api.NewServer(cfg.Server, cfg.API, sqliteStore, routerSvc, reservationMgr, passiveCollector)

	tgBot, err := setupTelegramBot(cfg, settingsStore, sqliteStore, routerSvc, accountManager, loader, routerConfig)
	if err != nil {
		log.Printf("Telegram setup warning: %v", err)
	}

	if cfg.Telegram.Enabled && !cfg.Alerts.Enabled {
		cfg.Alerts.Enabled = true
	}

	var alertSvc *alerts.Service
	var alertCancel context.CancelFunc
	if cfg.Telegram.Enabled && cfg.Alerts.Enabled && tgBot != nil && tgBot.IsEnabled() {
		alertCfg := alerts.Config{
			Enabled:            cfg.Alerts.Enabled,
			Thresholds:         cfg.Alerts.Thresholds,
			Debounce:           cfg.Alerts.Debounce,
			DailyDigestEnabled: cfg.Alerts.DailyDigestEnabled,
			DailyDigestTime:    cfg.Alerts.DailyDigestTime,
			Timezone:           cfg.Alerts.Timezone,
			RateLimitPerMinute: cfg.Alerts.RateLimitPerMinute,
			ShutdownTimeout:    cfg.Alerts.ShutdownTimeout,
		}
		alertSvc = alerts.NewService(alertCfg, tgBot)
		alertSvc.Start()
		tgBot.SetThresholdsCallback(func(warning, switchVal, critical float64) error {
			if routerSvc != nil {
				if current := routerSvc.GetConfig(); current != nil {
					newCfg := *current
					newCfg.WarningThreshold = warning
					newCfg.SwitchThreshold = switchVal
					newCfg.CriticalThreshold = critical
					routerSvc.UpdateConfig(newCfg)
				}
			}
			alertSvc.UpdateThresholds([]float64{warning, critical})
			return nil
		})

		tgBot.SetMuteCallback(func(duration time.Duration) error {
			alertSvc.MuteAlerts(duration, "telegram")
			return nil
		})
		tgBot.SetAlertsCallback(func() ([]telegram.ActiveAlert, error) {
			return []telegram.ActiveAlert{}, nil
		})

		alertCtx, cancel := context.WithCancel(context.Background())
		alertCancel = cancel
		startAlertLoop(alertCtx, alertSvc, sqliteStore, envDuration("QUOTAGUARD_ALERT_INTERVAL", time.Minute))
		startAccountAvailabilityLoop(
			alertCtx,
			alertSvc,
			sqliteStore,
			settingsStore,
			providerFetcher,
			envDuration("QUOTAGUARD_ACCOUNT_CHECK_INTERVAL", 3*time.Minute),
			envDuration("QUOTAGUARD_ACCOUNT_CHECK_TIMEOUT", 12*time.Second),
		)
	}

	// Setup graceful shutdown with all components
	setupGracefulShutdown(server, tgBot, activeCollector, alertSvc, alertCancel, serveFlags.Timeout)

	// Determine address
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.HTTPPort)

	if cfg.Server.TLS.Enabled {
		log.Printf("Starting QuotaGuard HTTPS server on %s", addr)
		log.Printf("TLS cert: %s, key: %s, min_version: %s", cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile, cfg.Server.TLS.MinVersion)
	} else {
		log.Printf("Starting QuotaGuard HTTP server on %s", addr)
	}
	log.Printf("Database: %s (WAL mode enabled)", globalFlags.DBPath)

	if err := server.Run(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// validateTLSConfig validates TLS configuration
func validateTLSConfig(tls config.TLSConfig) error {
	if tls.CertFile == "" {
		return fmt.Errorf("TLS certificate file is required when TLS is enabled")
	}
	if tls.KeyFile == "" {
		return fmt.Errorf("TLS key file is required when TLS is enabled")
	}

	// Check if certificate file exists
	if _, err := os.Stat(tls.CertFile); os.IsNotExist(err) {
		return fmt.Errorf("TLS certificate file does not exist: %s", tls.CertFile)
	}

	// Check if key file exists
	if _, err := os.Stat(tls.KeyFile); os.IsNotExist(err) {
		return fmt.Errorf("TLS key file does not exist: %s", tls.KeyFile)
	}

	// Validate TLS version
	if tls.MinVersion != "" && tls.MinVersion != "1.2" && tls.MinVersion != "1.3" {
		return fmt.Errorf("TLS min_version must be either \"1.2\" or \"1.3\", got: %s", tls.MinVersion)
	}

	return nil
}

// setupGracefulShutdown handles graceful shutdown of all components
func setupGracefulShutdown(server *api.Server, bot *telegram.Bot, active *collector.ActiveCollector, alertsSvc *alerts.Service, alertsCancel context.CancelFunc, timeout time.Duration) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)

		// Create context with timeout for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Shutdown server (stops router, collector, reservations, store)
		log.Println("Shutting down API server...")
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Error during server shutdown: %v", err)
		}

		if bot != nil {
			if err := bot.Stop(); err != nil {
				log.Printf("Error stopping telegram bot: %v", err)
			}
		}
		if active != nil {
			if err := active.Stop(); err != nil {
				log.Printf("Error stopping active collector: %v", err)
			}
		}
		if alertsCancel != nil {
			alertsCancel()
		}
		if alertsSvc != nil {
			if err := alertsSvc.Stop(); err != nil {
				log.Printf("Error stopping alerts service: %v", err)
			}
		}

		log.Println("Graceful shutdown completed")
		os.Exit(0)
	}()
}

func startAlertLoop(ctx context.Context, svc *alerts.Service, s store.Store, interval time.Duration) {
	if svc == nil || s == nil {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rawAccounts := s.ListEnabledAccounts()
				accounts := make([]models.Account, 0, len(rawAccounts))
				for _, acc := range rawAccounts {
					if acc == nil {
						continue
					}
					accounts = append(accounts, *acc)
				}
				rawQuotas := s.ListQuotas()
				quotas := make([]models.QuotaInfo, 0, len(rawQuotas))
				for _, quota := range rawQuotas {
					if quota == nil {
						continue
					}
					quotas = append(quotas, *quota)
				}
				alertsList := svc.CheckThresholds(accounts, quotas)
				if len(alertsList) == 0 {
					continue
				}
				_ = svc.ProcessAlerts(alertsList)
			}
		}
	}()
}

func startAccountAvailabilityLoop(
	ctx context.Context,
	svc *alerts.Service,
	s store.Store,
	settings store.SettingsStore,
	fetcher collector.QuotaFetcher,
	defaultInterval time.Duration,
	defaultTimeout time.Duration,
) {
	if svc == nil || s == nil || fetcher == nil {
		return
	}
	if defaultInterval <= 0 {
		defaultInterval = 3 * time.Minute
	}
	if defaultTimeout <= 0 {
		defaultTimeout = 12 * time.Second
	}

	type state struct {
		unhealthy bool
		lastError string
	}
	states := make(map[string]state)
	interval, timeout := resolveAccountCheckConfig(settings, defaultInterval, defaultTimeout)
	timer := time.NewTimer(interval)

	go func() {
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				interval, timeout = resolveAccountCheckConfig(settings, defaultInterval, defaultTimeout)
				for _, acc := range s.ListEnabledAccounts() {
					if acc == nil || acc.ProviderType == "" || acc.CredentialsRef == "" {
						continue
					}

					checkCtx, cancel := context.WithTimeout(ctx, timeout)
					_, err := fetcher.FetchQuota(checkCtx, acc.ID)
					cancel()

					prev := states[acc.ID]
					if err == nil {
						if prev.unhealthy {
							msg := fmt.Sprintf(
								"Account recovered: %s (%s). Availability check passed.",
								acc.ID, acc.ProviderType,
							)
							_ = svc.ProcessAlert(alerts.Alert{
								ID:        availabilityAlertID(acc.ID),
								AccountID: acc.ID,
								Type:      alerts.AlertTypeError,
								Severity:  alerts.SeverityInfo,
								Message:   msg,
								Timestamp: time.Now(),
								Metadata: map[string]interface{}{
									"provider_type": acc.ProviderType,
								},
							})
						}
						states[acc.ID] = state{}
						continue
					}

					if !isAccountAuthFailure(err) {
						continue
					}

					errText := strings.TrimSpace(err.Error())
					if !prev.unhealthy || prev.lastError != errText {
						msg := fmt.Sprintf(
							"Account unavailable: %s (%s). Re-login required. Next check in %s. Error: %s",
							acc.ID, acc.ProviderType, interval.Truncate(time.Second), errText,
						)
						_ = svc.ProcessAlert(alerts.Alert{
							ID:        availabilityAlertID(acc.ID),
							AccountID: acc.ID,
							Type:      alerts.AlertTypeError,
							Severity:  alerts.SeverityCritical,
							Message:   msg,
							Timestamp: time.Now(),
							Metadata: map[string]interface{}{
								"provider_type": acc.ProviderType,
								"next_check_in": interval.String(),
							},
						})
					}
					states[acc.ID] = state{unhealthy: true, lastError: errText}
				}
				timer.Reset(interval)
			}
		}
	}()
}

func resolveAccountCheckConfig(settings store.SettingsStore, defaultInterval, defaultTimeout time.Duration) (time.Duration, time.Duration) {
	interval := defaultInterval
	timeout := defaultTimeout
	if settings != nil {
		if sec := settings.GetInt(store.SettingAccountCheckIntSec, int(defaultInterval.Seconds())); sec > 0 {
			interval = time.Duration(sec) * time.Second
		}
		if sec := settings.GetInt(store.SettingAccountCheckTOSec, int(defaultTimeout.Seconds())); sec > 0 {
			timeout = time.Duration(sec) * time.Second
		}
	}
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	if interval > 30*time.Minute {
		interval = 30 * time.Minute
	}
	if timeout < 3*time.Second {
		timeout = 3 * time.Second
	}
	if timeout > 60*time.Second {
		timeout = 60 * time.Second
	}
	return interval, timeout
}

func isAccountAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "status 401") ||
		strings.Contains(text, "status 403") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "invalid_grant") ||
		strings.Contains(text, "missing refresh_token") ||
		strings.Contains(text, "missing access_token") ||
		strings.Contains(text, "token expired") ||
		strings.Contains(text, "session status")
}

func availabilityAlertID(accountID string) string {
	return fmt.Sprintf("availability-%s-%d", accountID, time.Now().UnixNano())
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		return parsed
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
