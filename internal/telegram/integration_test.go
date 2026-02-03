package telegram

import (
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/require"
)

func TestBotIntegratorHandlers(t *testing.T) {
	api := &mockBotAPI{}
	bot := NewBot("token", 1, true, &BotOptions{BotAPI: api})
	bot.onGetStatus = func() (*SystemStatus, error) {
		return &SystemStatus{
			AccountsActive: 2,
			RouterStatus:   "ok",
			AvgLatency:     50 * time.Millisecond,
			LastUpdate:     time.Now(),
		}, nil
	}
	bot.onGetAlerts = func() ([]ActiveAlert, error) {
		return []ActiveAlert{{ID: "1", Severity: "warn", Message: "test", Time: time.Now()}}, nil
	}

	bi := NewBotIntegrator(bot)
	require.NotNil(t, bi)
	require.NotEmpty(t, bi.handlers)

	require.Contains(t, bi.handlers, "status")
	require.Contains(t, bi.handlers, "help")

	bi.handlers["status"](1, nil)
	bi.handlers["alerts"](1, nil)
	bi.handlers["fallback"](1, nil)
	bi.handlers["thresholds"](1, nil)
	bi.handlers["policy"](1, nil)
	bi.handlers["import"](1, nil)
	bi.handlers["export"](1, nil)
	bi.handlers["reload"](1, nil)
	bi.handlers["help"](1, nil)

	require.GreaterOrEqual(t, len(api.GetMessages()), 9)

	bi.HandleUpdate(tgbotapi.Update{})
}

func TestIntegrationSessionHelpers(t *testing.T) {
	bot := NewBot("token", 1, true, nil)
	bot.setSessionState(1, StateWaitingSwitch)
	require.Equal(t, StateWaitingSwitch, bot.getSessionState(1))

	bot.clearSessionState(1)
	require.Equal(t, StateIdle, bot.getSessionState(1))

	bot.setSessionData(1, "key", "value")
	require.Equal(t, "value", bot.getSessionData(1, "key"))
	require.Equal(t, "", bot.getSessionData(1, "missing"))
}

func TestIntegrationHelpers(t *testing.T) {
	text := formatKeyValue("Key", "Value")
	require.Contains(t, text, "Key")
	require.Contains(t, text, "Value")

	require.Equal(t, "7", itoa(7))
}
