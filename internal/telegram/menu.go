package telegram

const (
	menuRoot     = "menu:root"
	menuStatus   = "menu:status"
	menuQuota    = "menu:quota"
	menuQuick    = "menu:quick"
	menuRouting  = "menu:routing"
	menuFallback = "menu:fallback"
	menuAlerts   = "menu:alerts"
	menuSettings = "menu:settings"
	menuAccounts = "menu:accounts"
	menuChecks   = "menu:checks"
	menuConnect  = "menu:connect"
	menuHelp     = "menu:help"

	actionThresholds  = "action:thresholds"
	actionPolicy      = "action:policy"
	actionIgnoreEst   = "action:ignore_estimated"
	actionReload      = "action:reload"
	actionImport      = "action:import"
	actionAcctEnable  = "action:acct_enable"
	actionAcctDisable = "action:acct_disable"
	actionCheckInt    = "action:check_interval"
	actionCheckTO     = "action:check_timeout"
	actionLogin       = "action:login"
)

func mainMenuKeyboard() InlineKeyboard {
	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "📊 Статус", CallbackData: menuStatus},
				{Text: "📈 Квоты", CallbackData: menuQuota},
			},
			{
				{Text: "⚡ Быстрые действия", CallbackData: menuQuick},
			},
			{
				{Text: "🧭 Роутинг", CallbackData: menuRouting},
				{Text: "🔁 Фоллбэки", CallbackData: menuFallback},
			},
			{
				{Text: "🛡️ Алёрты", CallbackData: menuAlerts},
				{Text: "⚙️ Настройки", CallbackData: menuSettings},
			},
			{
				{Text: "ℹ️ Помощь", CallbackData: menuHelp},
			},
		},
	}
}

func routingMenuKeyboard() InlineKeyboard {
	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "⚡ Агрессивно (80/88/94)", CallbackData: actionThresholds + ":80,88,94"},
			},
			{
				{Text: "🧠 Balanced (85/90/95)", CallbackData: actionThresholds + ":85,90,95"},
			},
			{
				{Text: "🧯 Консервативно (90/95/98)", CallbackData: actionThresholds + ":90,95,98"},
			},
			{
				{Text: "🧠 Balanced", CallbackData: actionPolicy + ":balanced"},
				{Text: "🛡️ Safety", CallbackData: actionPolicy + ":safety"},
			},
			{
				{Text: "🚀 Performance", CallbackData: actionPolicy + ":performance"},
				{Text: "💸 Cost", CallbackData: actionPolicy + ":cost"},
			},
			{
				{Text: "📈 Квоты", CallbackData: menuQuota},
				{Text: "📊 Статус", CallbackData: menuStatus},
			},
			{
				{Text: "⚙️ Настройки", CallbackData: menuSettings},
			},
			{
				{Text: "⬅️ Меню", CallbackData: menuRoot},
			},
		},
	}
}

func quickActionsKeyboard() InlineKeyboard {
	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "📈 Квоты", CallbackData: menuQuota},
				{Text: "📊 Статус", CallbackData: menuStatus},
			},
			{
				{Text: "🛡️ Алёрты", CallbackData: menuAlerts},
				{Text: "🧭 Роутинг", CallbackData: menuRouting},
			},
			{
				{Text: "📥 Импорт аккаунтов", CallbackData: actionImport},
			},
			{
				{Text: "🔄 Перезагрузить конфиг", CallbackData: actionReload},
			},
			{
				{Text: "⬅️ Меню", CallbackData: menuRoot},
			},
		},
	}
}

func settingsMenuKeyboard(ignoreEstimated bool) InlineKeyboard {
	ignoreLabel := "✅ Игнорировать estimated"
	toggleValue := "off"
	if !ignoreEstimated {
		ignoreLabel = "☑️ Игнорировать estimated"
		toggleValue = "on"
	}

	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "👤 Управление аккаунтами", CallbackData: menuAccounts},
			},
			{
				{Text: "🩺 Проверка доступности", CallbackData: menuChecks},
			},
			{
				{Text: "➕ Подключить аккаунт", CallbackData: menuConnect},
			},
			{
				{Text: ignoreLabel, CallbackData: actionIgnoreEst + ":" + toggleValue},
			},
			{
				{Text: "🔄 Перезагрузить конфиг", CallbackData: actionReload},
			},
			{
				{Text: "⬅️ Меню", CallbackData: menuRoot},
			},
		},
	}
}

func accountChecksMenuKeyboard() InlineKeyboard {
	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "⏱ Интервал 1м", CallbackData: actionCheckInt + ":1m"},
				{Text: "⏱ Интервал 3м", CallbackData: actionCheckInt + ":3m"},
			},
			{
				{Text: "⏱ Интервал 5м", CallbackData: actionCheckInt + ":5m"},
			},
			{
				{Text: "⌛ Timeout 8с", CallbackData: actionCheckTO + ":8s"},
				{Text: "⌛ Timeout 12с", CallbackData: actionCheckTO + ":12s"},
			},
			{
				{Text: "⌛ Timeout 20с", CallbackData: actionCheckTO + ":20s"},
			},
			{
				{Text: "⬅️ Настройки", CallbackData: menuSettings},
			},
		},
	}
}

func connectAccountsMenuKeyboard() InlineKeyboard {
	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "🛰 Antigravity", CallbackData: actionLogin + ":antigravity"},
				{Text: "✨ Gemini", CallbackData: actionLogin + ":gemini"},
			},
			{
				{Text: "📥 Импорт после логина", CallbackData: actionImport},
			},
			{
				{Text: "⬅️ Настройки", CallbackData: menuSettings},
			},
		},
	}
}

func accountsMenuKeyboard(accounts []AccountControl, callbackKeys []string) InlineKeyboard {
	rows := make([][]InlineButton, 0, len(accounts)+2)
	for i, acc := range accounts {
		if i >= len(callbackKeys) {
			break
		}
		key := callbackKeys[i]
		label := "▶️ Включить"
		callback := actionAcctEnable + ":" + key
		if acc.Enabled {
			label = "⏸ На 1ч"
			callback = actionAcctDisable + ":" + key + ":1h"
		}
		rows = append(rows, []InlineButton{
			{Text: label, CallbackData: callback},
		})
	}
	rows = append(rows, []InlineButton{
		{Text: "🔄 Обновить", CallbackData: menuAccounts},
	})
	rows = append(rows, []InlineButton{
		{Text: "⬅️ Настройки", CallbackData: menuSettings},
	})
	return InlineKeyboard{Rows: rows}
}

func fallbackMenuKeyboard() InlineKeyboard {
	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "⬅️ Меню", CallbackData: menuRoot},
			},
		},
	}
}

func sectionKeyboard(refreshCallback string) InlineKeyboard {
	return InlineKeyboard{
		Rows: [][]InlineButton{
			{
				{Text: "🔄 Обновить", CallbackData: refreshCallback},
			},
			{
				{Text: "⬅️ Меню", CallbackData: menuRoot},
			},
		},
	}
}
