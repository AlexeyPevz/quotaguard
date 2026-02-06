package telegram

import (
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TGBotAPIClient adapts tgbotapi.BotAPI to the BotAPI interface.
type TGBotAPIClient struct {
	bot          *tgbotapi.BotAPI
	updateConfig tgbotapi.UpdateConfig
	mu           sync.Mutex
}

// NewTGBotAPIClient creates a new Telegram client using tgbotapi.
func NewTGBotAPIClient(token string) (*TGBotAPIClient, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	update := tgbotapi.NewUpdate(0)
	update.Timeout = 30

	return &TGBotAPIClient{
		bot:          bot,
		updateConfig: update,
	}, nil
}

// SendMessage sends a message to the specified chat.
func (c *TGBotAPIClient) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := c.bot.Send(msg)
	return err
}

// SendMessageWithParseMode sends a message with parse mode (HTML/MarkdownV2).
func (c *TGBotAPIClient) SendMessageWithParseMode(chatID int64, text string, parseMode string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = parseMode
	_, err := c.bot.Send(msg)
	return err
}

// SendMessageWithInlineKeyboard sends a message with an inline keyboard and parse mode.
func (c *TGBotAPIClient) SendMessageWithInlineKeyboard(chatID int64, text, parseMode string, keyboard InlineKeyboard) error {
	msg := tgbotapi.NewMessage(chatID, text)
	if parseMode != "" {
		msg.ParseMode = parseMode
	}
	if keyboard.HasButtons() {
		rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(keyboard.Rows))
		for _, row := range keyboard.Rows {
			btnRow := make([]tgbotapi.InlineKeyboardButton, 0, len(row))
			for _, btn := range row {
				if btn.URL != "" {
					btnRow = append(btnRow, tgbotapi.NewInlineKeyboardButtonURL(btn.Text, btn.URL))
					continue
				}
				btnRow = append(btnRow, tgbotapi.NewInlineKeyboardButtonData(btn.Text, btn.CallbackData))
			}
			rows = append(rows, btnRow)
		}
		msg.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	_, err := c.bot.Send(msg)
	return err
}

// GetUpdates fetches new updates and converts them to Message.
func (c *TGBotAPIClient) GetUpdates() ([]Message, error) {
	c.mu.Lock()
	updates, err := c.bot.GetUpdates(c.updateConfig)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if len(updates) > 0 {
		c.updateConfig.Offset = updates[len(updates)-1].UpdateID + 1
	}
	c.mu.Unlock()

	messages := make([]Message, 0, len(updates))
	for _, update := range updates {
		if update.Message != nil {
			messages = append(messages, Message{
				ID:        int64(update.Message.MessageID),
				ChatID:    update.Message.Chat.ID,
				Text:      update.Message.Text,
				Timestamp: time.Unix(int64(update.Message.Date), 0),
			})
		} else if update.CallbackQuery != nil && update.CallbackQuery.Message != nil {
			messages = append(messages, Message{
				ID:        int64(update.CallbackQuery.Message.MessageID),
				ChatID:    update.CallbackQuery.Message.Chat.ID,
				Text:      update.CallbackQuery.Data,
				Timestamp: time.Unix(int64(update.CallbackQuery.Message.Date), 0),
			})
		}
	}

	return messages, nil
}

var _ BotAPI = (*TGBotAPIClient)(nil)
