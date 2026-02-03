package cli

import (
	"encoding/json"
	"fmt"

	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/router"
	"github.com/quotaguard/quotaguard/internal/store"
)

func ensureSettingsDefaults(settings store.SettingsStore, cfg *config.Config) error {
	if settings == nil || cfg == nil {
		return nil
	}

	setFloatIfMissing(settings, store.SettingThresholdsWarning, cfg.Router.Thresholds.Warning)
	setFloatIfMissing(settings, store.SettingThresholdsSwitch, cfg.Router.Thresholds.Switch)
	setFloatIfMissing(settings, store.SettingThresholdsCritical, cfg.Router.Thresholds.Critical)
	setStringIfMissing(settings, store.SettingRoutingPolicy, "balanced")

	if cfg.Router.FallbackChains != nil {
		if _, ok := settings.Get(store.SettingFallbackChains); !ok {
			data, err := json.Marshal(cfg.Router.FallbackChains)
			if err != nil {
				return fmt.Errorf("marshal fallback chains: %w", err)
			}
			if err := settings.Set(store.SettingFallbackChains, string(data)); err != nil {
				return fmt.Errorf("store fallback chains: %w", err)
			}
		}
	}

	if cfg.Telegram.BotToken != "" {
		setStringIfMissing(settings, store.SettingTelegramBotToken, cfg.Telegram.BotToken)
	}
	if cfg.Telegram.ChatID != 0 {
		setIntIfMissing(settings, store.SettingTelegramChatID, int(cfg.Telegram.ChatID))
	}

	return nil
}

func applySettingsToRouterConfig(settings store.SettingsStore, cfg *router.Config) error {
	if settings == nil || cfg == nil {
		return nil
	}

	cfg.WarningThreshold = settings.GetFloat(store.SettingThresholdsWarning, cfg.WarningThreshold)
	cfg.SwitchThreshold = settings.GetFloat(store.SettingThresholdsSwitch, cfg.SwitchThreshold)
	cfg.CriticalThreshold = settings.GetFloat(store.SettingThresholdsCritical, cfg.CriticalThreshold)

	if policy, ok := settings.Get(store.SettingRoutingPolicy); ok && policy != "" {
		cfg.DefaultPolicy = policy
	}

	if raw, ok := settings.Get(store.SettingFallbackChains); ok && raw != "" {
		var chains map[string][]string
		if err := json.Unmarshal([]byte(raw), &chains); err != nil {
			return fmt.Errorf("invalid fallback_chains JSON in settings: %w", err)
		}
		cfg.FallbackChains = chains
	}

	return nil
}

func applySettingsToTelegramConfig(settings store.SettingsStore, cfg *config.TelegramConfig) {
	if settings == nil || cfg == nil {
		return
	}

	if token, ok := settings.Get(store.SettingTelegramBotToken); ok && token != "" {
		cfg.BotToken = token
	}
	if raw, ok := settings.Get(store.SettingTelegramChatID); ok && raw != "" {
		chatID := settings.GetInt(store.SettingTelegramChatID, 0)
		if chatID != 0 {
			cfg.ChatID = int64(chatID)
		}
	}
}

func setStringIfMissing(settings store.SettingsStore, key, value string) {
	if value == "" {
		return
	}
	if _, ok := settings.Get(key); ok {
		return
	}
	_ = settings.Set(key, value)
}

func setFloatIfMissing(settings store.SettingsStore, key string, value float64) {
	if _, ok := settings.Get(key); ok {
		return
	}
	_ = settings.SetFloat(key, value)
}

func setIntIfMissing(settings store.SettingsStore, key string, value int) {
	if value == 0 {
		return
	}
	if _, ok := settings.Get(key); ok {
		return
	}
	_ = settings.SetInt(key, value)
}
