package telegram

// InlineButton represents a single inline keyboard button.
type InlineButton struct {
	Text         string
	CallbackData string
	URL          string
}

// InlineKeyboard represents an inline keyboard layout.
type InlineKeyboard struct {
	Rows [][]InlineButton
}

// InlineKeyboardSender allows sending messages with inline keyboards.
type InlineKeyboardSender interface {
	SendMessageWithInlineKeyboard(chatID int64, text, parseMode string, keyboard InlineKeyboard) error
}

// HasButtons indicates whether the keyboard has any buttons.
func (k InlineKeyboard) HasButtons() bool {
	return len(k.Rows) > 0
}
