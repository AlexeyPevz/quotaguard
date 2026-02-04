package telegram

import (
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Notify sends a one-off message without requiring a running bot instance.
func Notify(token string, chatID int64, text string) {
	token = strings.TrimSpace(token)
	if token == "" || chatID == 0 || strings.TrimSpace(text) == "" {
		return
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return
	}
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, _ = bot.Send(msg)
}
