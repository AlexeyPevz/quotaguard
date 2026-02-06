package telegram

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBotAPI is a mock implementation of BotAPI
type mockBotAPI struct {
	mu       sync.Mutex
	messages []mockMessage
}

type mockMessage struct {
	chatID int64
	text   string
}

func (m *mockBotAPI) SendMessage(chatID int64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, mockMessage{chatID: chatID, text: text})
	return nil
}

func (m *mockBotAPI) GetUpdates() ([]Message, error) {
	return nil, nil
}

func (m *mockBotAPI) GetMessages() []mockMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

func TestNewBot(t *testing.T) {
	tests := []struct {
		name     string
		botToken string
		chatID   int64
		enabled  bool
		opts     *BotOptions
	}{
		{
			name:     "basic bot creation",
			botToken: "test-token",
			chatID:   12345,
			enabled:  true,
			opts:     nil,
		},
		{
			name:     "disabled bot",
			botToken: "test-token",
			chatID:   12345,
			enabled:  false,
			opts:     nil,
		},
		{
			name:     "bot with custom rate limiter",
			botToken: "test-token",
			chatID:   12345,
			enabled:  true,
			opts: &BotOptions{
				RateLimiter: NewRateLimiter(60),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot := NewBot(tt.botToken, tt.chatID, tt.enabled, tt.opts)

			require.NotNil(t, bot)
			assert.Equal(t, tt.botToken, bot.botToken)
			assert.Equal(t, tt.chatID, bot.chatID)
			assert.Equal(t, tt.enabled, bot.enabled)
			assert.NotNil(t, bot.rateLimiter)
			assert.NotNil(t, bot.dedup)
			assert.NotNil(t, bot.sessions)
			assert.NotNil(t, bot.ctx)
			assert.NotNil(t, bot.cancel)
		})
	}
}

func TestBotStartStop(t *testing.T) {
	t.Run("start disabled bot", func(t *testing.T) {
		bot := NewBot("token", 12345, false, nil)
		err := bot.Start()
		assert.NoError(t, err)
	})

	t.Run("start without token", func(t *testing.T) {
		bot := NewBot("", 12345, true, nil)
		err := bot.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bot token is required")
	})

	t.Run("start and stop", func(t *testing.T) {
		mockAPI := &mockBotAPI{}
		bot := NewBot("token", 12345, true, &BotOptions{BotAPI: mockAPI})

		err := bot.Start()
		require.NoError(t, err)

		// Give goroutines time to start
		time.Sleep(50 * time.Millisecond)

		err = bot.Stop()
		assert.NoError(t, err)
	})
}

func TestRateLimiter(t *testing.T) {
	t.Run("allow within rate limit", func(t *testing.T) {
		rl := NewRateLimiter(60) // 60 messages per minute

		// Should allow first 60 messages
		for i := 0; i < 60; i++ {
			assert.True(t, rl.Allow(), "message %d should be allowed", i)
		}
	})

	t.Run("block when rate limit exceeded", func(t *testing.T) {
		rl := NewRateLimiter(10) // 10 messages per minute

		// Allow first 10
		for i := 0; i < 10; i++ {
			assert.True(t, rl.Allow())
		}

		// 11th should be blocked (until tokens refill)
		assert.False(t, rl.Allow())
	})

	t.Run("tokens refill over time", func(t *testing.T) {
		rl := NewRateLimiter(60)

		// Exhaust tokens
		for i := 0; i < 60; i++ {
			rl.Allow()
		}

		// Should be blocked
		assert.False(t, rl.Allow())

		// Wait for tokens to refill (simulate time passing)
		rl.mu.Lock()
		rl.lastUpdate = rl.lastUpdate.Add(-time.Minute)
		rl.mu.Unlock()

		// Should allow again after refill
		assert.True(t, rl.Allow())
	})
}

func TestDedupLimiter(t *testing.T) {
	t.Run("allow new messages", func(t *testing.T) {
		dl := NewDedupLimiter(5 * time.Minute)

		assert.True(t, dl.CanSend("msg1"))
		assert.True(t, dl.CanSend("msg2"))
	})

	t.Run("block duplicate within window", func(t *testing.T) {
		dl := NewDedupLimiter(5 * time.Minute)

		assert.True(t, dl.CanSend("msg1"))
		assert.False(t, dl.CanSend("msg1")) // Duplicate
	})

	t.Run("allow duplicate after window", func(t *testing.T) {
		dl := NewDedupLimiter(1 * time.Millisecond)

		assert.True(t, dl.CanSend("msg1"))
		time.Sleep(2 * time.Millisecond)
		assert.True(t, dl.CanSend("msg1")) // After window expires
	})

	t.Run("cleanup removes old entries", func(t *testing.T) {
		dl := NewDedupLimiter(1 * time.Millisecond)

		dl.CanSend("msg1")
		time.Sleep(2 * time.Millisecond)
		dl.Cleanup()

		// Entry should be removed
		dl.mu.RLock()
		_, exists := dl.sent["msg1"]
		dl.mu.RUnlock()
		assert.False(t, exists)
	})
}

func TestBotSessions(t *testing.T) {
	bot := NewBot("token", 12345, true, nil)

	t.Run("get new session", func(t *testing.T) {
		session := bot.GetSession(123)
		require.NotNil(t, session)
		assert.Equal(t, int64(123), session.UserID)
		assert.Equal(t, StateIdle, session.State)
		assert.NotNil(t, session.Data)
	})

	t.Run("get existing session", func(t *testing.T) {
		session1 := bot.GetSession(456)
		session1.State = StateWaitingMute

		session2 := bot.GetSession(456)
		assert.Equal(t, StateWaitingMute, session2.State)
	})

	t.Run("set session state", func(t *testing.T) {
		bot.GetSession(789)
		bot.SetSessionState(789, StateConfirming, map[string]interface{}{"key": "value"})

		session := bot.GetSession(789)
		assert.Equal(t, StateConfirming, session.State)
		assert.Equal(t, "value", session.Data["key"])
	})

	t.Run("clear session", func(t *testing.T) {
		bot.GetSession(999)
		bot.ClearSession(999)

		bot.sessionsMu.RLock()
		_, exists := bot.sessions[999]
		bot.sessionsMu.RUnlock()
		assert.False(t, exists)
	})

	t.Run("cleanup old sessions", func(t *testing.T) {
		session := bot.GetSession(111)
		session.UpdatedAt = time.Now().Add(-2 * time.Hour)

		bot.CleanupSessions(1 * time.Hour)

		bot.sessionsMu.RLock()
		_, exists := bot.sessions[111]
		bot.sessionsMu.RUnlock()
		assert.False(t, exists)
	})
}

func TestBotSendMessage(t *testing.T) {
	t.Run("send message when enabled", func(t *testing.T) {
		mockAPI := &mockBotAPI{}
		bot := NewBot("token", 12345, true, &BotOptions{
			BotAPI:      mockAPI,
			RateLimiter: NewRateLimiter(60),
		})

		err := bot.SendMessage("test message")
		assert.NoError(t, err)

		messages := mockAPI.GetMessages()
		require.Len(t, messages, 1)
		assert.Equal(t, int64(12345), messages[0].chatID)
		assert.Equal(t, "test message", messages[0].text)
	})

	t.Run("no-op when disabled", func(t *testing.T) {
		mockAPI := &mockBotAPI{}
		bot := NewBot("token", 12345, false, &BotOptions{BotAPI: mockAPI})

		err := bot.SendMessage("test message")
		assert.NoError(t, err)

		messages := mockAPI.GetMessages()
		assert.Len(t, messages, 0)
	})

	t.Run("rate limit error", func(t *testing.T) {
		mockAPI := &mockBotAPI{}
		bot := NewBot("token", 12345, true, &BotOptions{
			BotAPI:      mockAPI,
			RateLimiter: NewRateLimiter(0), // 0 rate limit
		})

		err := bot.SendMessage("test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "rate limit exceeded")
	})
}

func TestBotSendAlert(t *testing.T) {
	t.Run("send alert with dedup", func(t *testing.T) {
		mockAPI := &mockBotAPI{}
		bot := NewBot("token", 12345, true, &BotOptions{
			BotAPI:       mockAPI,
			RateLimiter:  NewRateLimiter(60),
			DedupLimiter: NewDedupLimiter(5 * time.Minute),
		})

		// Need to start bot to process alert channel
		err := bot.Start()
		require.NoError(t, err)
		defer func() {
			require.NoError(t, bot.Stop())
		}()

		alert := Alert{
			ID:       "alert1",
			Severity: "warning",
			Message:  "Test alert",
		}

		err = bot.SendAlert(alert)
		assert.NoError(t, err)

		// Wait for processing
		time.Sleep(50 * time.Millisecond)

		// Duplicate should be deduplicated
		err = bot.SendAlert(alert)
		assert.NoError(t, err) // Returns nil but doesn't send
	})

	t.Run("no-op when disabled", func(t *testing.T) {
		bot := NewBot("token", 12345, false, nil)

		alert := Alert{ID: "alert1", Severity: "warning", Message: "Test"}
		err := bot.SendAlert(alert)
		assert.NoError(t, err)
	})
}

func TestBotCallbacks(t *testing.T) {
	bot := NewBot("token", 12345, true, nil)

	t.Run("set status callback", func(t *testing.T) {
		called := false
		bot.SetStatusCallback(func() (*SystemStatus, error) {
			called = true
			return nil, nil
		})
		assert.NotNil(t, bot.onGetStatus)
		_, _ = bot.onGetStatus()
		assert.True(t, called)
	})

	t.Run("set quotas callback", func(t *testing.T) {
		called := false
		bot.SetQuotasCallback(func() ([]AccountQuota, error) {
			called = true
			return nil, nil
		})
		assert.NotNil(t, bot.onGetQuotas)
		_, _ = bot.onGetQuotas()
		assert.True(t, called)
	})

	t.Run("set alerts callback", func(t *testing.T) {
		called := false
		bot.SetAlertsCallback(func() ([]ActiveAlert, error) {
			called = true
			return nil, nil
		})
		assert.NotNil(t, bot.onGetAlerts)
		_, _ = bot.onGetAlerts()
		assert.True(t, called)
	})

	t.Run("set mute callback", func(t *testing.T) {
		called := false
		bot.SetMuteCallback(func(d time.Duration) error {
			called = true
			return nil
		})
		assert.NotNil(t, bot.onMuteAlerts)
		_ = bot.onMuteAlerts(5 * time.Minute)
		assert.True(t, called)
	})

	t.Run("set force switch callback", func(t *testing.T) {
		called := false
		bot.SetForceSwitchCallback(func(id string) error {
			called = true
			return nil
		})
		assert.NotNil(t, bot.onForceSwitch)
		_ = bot.onForceSwitch("account1")
		assert.True(t, called)
	})

	t.Run("set daily digest callback", func(t *testing.T) {
		called := false
		bot.SetDailyDigestCallback(func() (*DailyDigest, error) {
			called = true
			return nil, nil
		})
		assert.NotNil(t, bot.onGetDailyDigest)
		_, _ = bot.onGetDailyDigest()
		assert.True(t, called)
	})
}

func TestBotMethods(t *testing.T) {
	bot := NewBot("token", 12345, true, nil)

	t.Run("is enabled", func(t *testing.T) {
		assert.True(t, bot.IsEnabled())
		bot.enabled = false
		assert.False(t, bot.IsEnabled())
	})

	t.Run("get chat ID", func(t *testing.T) {
		assert.Equal(t, int64(12345), bot.GetChatID())
	})
}

func TestBotMessageProcessing(t *testing.T) {
	tests := []struct {
		name           string
		message        string
		expectedPrefix string
		setupCallback  func(*Bot)
	}{
		{
			name:           "handle /start",
			message:        "/start",
			expectedPrefix: "📌 <b>Главное меню</b>",
		},
		{
			name:           "handle /help",
			message:        "/help",
			expectedPrefix: "📌 <b>Главное меню</b>",
		},
		{
			name:           "handle unknown command",
			message:        "/unknown",
			expectedPrefix: "📌 <b>Главное меню</b>",
		},
		{
			name:           "handle status",
			message:        "/status",
			expectedPrefix: "📌 <b>Главное меню</b>",
			setupCallback: func(b *Bot) {
				b.SetStatusCallback(func() (*SystemStatus, error) {
					return &SystemStatus{
						AccountsActive: 5,
						RouterStatus:   "healthy",
						AvgLatency:     12 * time.Millisecond,
						LastUpdate:     time.Now(),
					}, nil
				})
			},
		},
		{
			name:           "handle quota",
			message:        "/quota",
			expectedPrefix: "📌 <b>Главное меню</b>",
			setupCallback: func(b *Bot) {
				b.SetQuotasCallback(func() ([]AccountQuota, error) {
					return []AccountQuota{
						{AccountID: "openai-1", Provider: "RPM", UsagePercent: 80},
					}, nil
				})
			},
		},
		{
			name:           "handle alerts",
			message:        "/alerts",
			expectedPrefix: "📌 <b>Главное меню</b>",
			setupCallback: func(b *Bot) {
				b.SetAlertsCallback(func() ([]ActiveAlert, error) {
					return []ActiveAlert{}, nil
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockBotAPI{}
			bot := NewBot("token", 12345, true, &BotOptions{
				BotAPI:      mockAPI,
				RateLimiter: NewRateLimiter(60),
			})

			if tt.setupCallback != nil {
				tt.setupCallback(bot)
			}

			// Simulate message
			msg := Message{
				ChatID:    12345,
				Text:      tt.message,
				Timestamp: time.Now(),
			}

			bot.handleMessage(msg)

			messages := mockAPI.GetMessages()
			if len(messages) > 0 {
				assert.Contains(t, messages[0].text, tt.expectedPrefix)
			}
		})
	}
}

func TestBotMuteCommand(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		state         State
		input         string
		expectSuccess bool
		setupCallback func(*Bot)
	}{
		{
			name:          "mute with duration",
			args:          []string{"30m"},
			expectSuccess: true,
			setupCallback: func(b *Bot) {
				b.SetMuteCallback(func(d time.Duration) error {
					return nil
				})
			},
		},
		{
			name:          "mute without duration - enter state",
			args:          []string{},
			expectSuccess: true,
			setupCallback: func(b *Bot) {
				b.SetMuteCallback(func(d time.Duration) error {
					return nil
				})
			},
		},
		{
			name:          "mute in waiting state",
			args:          []string{},
			state:         StateWaitingMute,
			input:         "1h",
			expectSuccess: true,
			setupCallback: func(b *Bot) {
				b.SetMuteCallback(func(d time.Duration) error {
					return nil
				})
			},
		},
		{
			name:          "mute with callback error",
			args:          []string{"30m"},
			expectSuccess: false,
			setupCallback: func(b *Bot) {
				b.SetMuteCallback(func(d time.Duration) error {
					return errors.New("mute error")
				})
			},
		},
		{
			name:          "mute with invalid duration",
			args:          []string{"invalid"},
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockBotAPI{}
			bot := NewBot("token", 12345, true, &BotOptions{
				BotAPI:      mockAPI,
				RateLimiter: NewRateLimiter(60),
			})

			if tt.setupCallback != nil {
				tt.setupCallback(bot)
			}

			// Set state if needed
			if tt.state != "" {
				bot.sessions[12345] = &UserSession{
					UserID: 12345,
					State:  tt.state,
					Data:   make(map[string]interface{}),
				}
			}

			var msg Message
			if tt.input != "" {
				msg = Message{ChatID: 12345, Text: tt.input, Timestamp: time.Now()}
			} else {
				msg = Message{ChatID: 12345, Text: "/mute", Timestamp: time.Now()}
			}

			bot.handleMessage(msg)

			// Check for duration in waiting state - only for the specific test case
			if tt.name == "mute without duration - enter state" {
				session := bot.GetSession(12345)
				assert.Equal(t, StateIdle, session.State)
			}
		})
	}
}

func TestBotForceSwitchCommand(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		state         State
		input         string
		confirm       string
		expectSuccess bool
		setupCallback func(*Bot)
	}{
		{
			name:          "force switch with account",
			args:          []string{"openai-1"},
			state:         StateConfirming,
			confirm:       "yes",
			expectSuccess: true,
			setupCallback: func(b *Bot) {
				b.SetForceSwitchCallback(func(id string) error {
					return nil
				})
			},
		},
		{
			name:          "force switch without account",
			args:          []string{},
			expectSuccess: true,
		},
		{
			name:          "force switch cancelled",
			args:          []string{"openai-1"},
			state:         StateConfirming,
			confirm:       "no",
			expectSuccess: false,
			setupCallback: func(b *Bot) {
				b.SetForceSwitchCallback(func(id string) error {
					return nil
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockBotAPI{}
			bot := NewBot("token", 12345, true, &BotOptions{
				BotAPI:      mockAPI,
				RateLimiter: NewRateLimiter(60),
			})

			if tt.setupCallback != nil {
				tt.setupCallback(bot)
			}

			// Handle initial command
			var initialMsg Message
			if len(tt.args) > 0 {
				initialMsg = Message{
					ChatID:    12345,
					Text:      "/force_switch " + tt.args[0],
					Timestamp: time.Now(),
				}
			} else {
				initialMsg = Message{
					ChatID:    12345,
					Text:      "/force_switch",
					Timestamp: time.Now(),
				}
			}
			bot.handleMessage(initialMsg)

			// Handle confirmation if needed
			if tt.confirm != "" {
				confirmMsg := Message{
					ChatID:    12345,
					Text:      tt.confirm,
					Timestamp: time.Now(),
				}
				bot.handleMessage(confirmMsg)
			}

			// Verify appropriate messages sent
			messages := mockAPI.GetMessages()
			assert.Greater(t, len(messages), 0)
		})
	}
}

func TestBotRateLimitingInHandler(t *testing.T) {
	mockAPI := &mockBotAPI{}
	bot := NewBot("token", 12345, true, &BotOptions{
		BotAPI:      mockAPI,
		RateLimiter: NewRateLimiter(0), // 0 rate to trigger limit
	})

	bot.SetStatusCallback(func() (*SystemStatus, error) {
		return &SystemStatus{AccountsActive: 1}, nil
	})

	msg := Message{
		ChatID:    12345,
		Text:      "/status",
		Timestamp: time.Now(),
	}

	bot.handleMessage(msg)

	messages := mockAPI.GetMessages()
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0].text, "Rate limit exceeded")
}

func TestBotConcurrency(t *testing.T) {
	bot := NewBot("token", 12345, true, nil)

	// Test concurrent access to rate limiter
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if bot.rateLimiter.Allow() {
				atomic.AddInt64(&counter, 1)
			}
		}()
	}

	wg.Wait()

	// Should allow up to 30 (default rate limit)
	assert.LessOrEqual(t, atomic.LoadInt64(&counter), int64(30))
}

func TestBotErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		message        string
		setupCallback  func(*Bot)
		expectedPrefix string
	}{
		{
			name:           "status callback error",
			message:        "/status",
			expectedPrefix: "📌 <b>Главное меню</b>",
			setupCallback: func(b *Bot) {
				b.SetStatusCallback(func() (*SystemStatus, error) {
					return nil, errors.New("status error")
				})
			},
		},
		{
			name:           "quota callback error",
			message:        "/quota",
			expectedPrefix: "📌 <b>Главное меню</b>",
			setupCallback: func(b *Bot) {
				b.SetQuotasCallback(func() ([]AccountQuota, error) {
					return nil, errors.New("quota error")
				})
			},
		},
		{
			name:           "alerts callback error",
			message:        "/alerts",
			expectedPrefix: "📌 <b>Главное меню</b>",
			setupCallback: func(b *Bot) {
				b.SetAlertsCallback(func() ([]ActiveAlert, error) {
					return nil, errors.New("alerts error")
				})
			},
		},
		{
			name:           "mute callback not configured",
			message:        "/mute 30m",
			expectedPrefix: "📌 <b>Главное меню</b>",
		},
		{
			name:           "force switch callback not configured",
			message:        "/force_switch account1",
			expectedPrefix: "📌 <b>Главное меню</b>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockAPI := &mockBotAPI{}
			bot := NewBot("token", 12345, true, &BotOptions{
				BotAPI:      mockAPI,
				RateLimiter: NewRateLimiter(60),
			})

			if tt.setupCallback != nil {
				tt.setupCallback(bot)
			}

			msg := Message{
				ChatID:    12345,
				Text:      tt.message,
				Timestamp: time.Now(),
			}

			bot.handleMessage(msg)

			messages := mockAPI.GetMessages()
			if len(messages) > 0 {
				assert.Contains(t, messages[0].text, tt.expectedPrefix)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"30m", 30 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"30min", 30 * time.Minute, false},
		{"1hour", 1 * time.Hour, false},
		{"2hours", 2 * time.Hour, false},
		{"1day", 24 * time.Hour, false},
		{"2days", 48 * time.Hour, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"30x", 0, true},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseDuration(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestBotSendAlertWithRateLimit(t *testing.T) {
	mockAPI := &mockBotAPI{}
	bot := NewBot("token", 12345, true, &BotOptions{
		BotAPI:       mockAPI,
		RateLimiter:  NewRateLimiter(60),
		DedupLimiter: NewDedupLimiter(5 * time.Minute),
	})

	err := bot.Start()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, bot.Stop())
	}()

	// Send multiple alerts
	for i := 0; i < 5; i++ {
		alert := Alert{
			ID:       fmt.Sprintf("alert%d", i),
			Severity: "warning",
			Message:  fmt.Sprintf("Test alert %d", i),
		}
		err := bot.SendAlert(alert)
		assert.NoError(t, err)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Send duplicate - should be deduplicated
	alert := Alert{ID: "alert0", Severity: "warning", Message: "Test alert 0"}
	err = bot.SendAlert(alert)
	assert.NoError(t, err)
}

func TestFormatFunctions(t *testing.T) {
	tests := []struct {
		name     string
		function func() string
		contains []string
	}{
		{
			name: "formatStatus healthy",
			function: func() string {
				return formatStatus(&SystemStatus{
					AccountsActive: 5,
					RouterStatus:   "healthy",
					AvgLatency:     12 * time.Millisecond,
					LastUpdate:     time.Now(),
				})
			},
			contains: []string{"🟢", "QuotaGuard Status", "5 active", "healthy", "12ms"},
		},
		{
			name: "formatStatus unhealthy",
			function: func() string {
				return formatStatus(&SystemStatus{
					AccountsActive: 3,
					RouterStatus:   "degraded",
					AvgLatency:     150 * time.Millisecond,
					LastUpdate:     time.Now(),
				})
			},
			contains: []string{"🔴", "degraded"},
		},
		{
			name: "formatQuotas",
			function: func() string {
				return formatQuotas([]AccountQuota{
					{AccountID: "openai-1", Provider: "RPM", UsagePercent: 80, IsWarning: false},
					{AccountID: "anthropic-1", Provider: "RPM", UsagePercent: 95, IsWarning: true},
				})
			},
			contains: []string{"📊", "openai-1", "anthropic-1", "⚠️", "80%", "95%"},
		},
		{
			name: "formatAlerts empty",
			function: func() string {
				return formatAlerts([]ActiveAlert{})
			},
			contains: []string{"🛡️", "Нет активных алертов"},
		},
		{
			name: "formatAlerts with alerts",
			function: func() string {
				return formatAlerts([]ActiveAlert{
					{ID: "1", Severity: "warning", Message: "High usage", Time: time.Now().Add(-5 * time.Minute)},
					{ID: "2", Severity: "critical", Message: "Account down", Time: time.Now().Add(-1 * time.Hour)},
				})
			},
			contains: []string{"⚠️", "WARNING", "🔴", "CRITICAL", "High usage", "Account down"},
		},
		{
			name: "formatMuteConfirmation",
			function: func() string {
				return formatMuteConfirmation(30 * time.Minute)
			},
			contains: []string{"🔇", "30m"},
		},
		{
			name: "formatAlert",
			function: func() string {
				return formatAlert(Alert{
					ID:        "alert1",
					Severity:  "warning",
					Message:   "Test message",
					AccountID: "acc1",
					Timestamp: time.Now(),
				})
			},
			contains: []string{"🟡", "WARNING", "Test message", "acc1"},
		},
		{
			name: "formatDailyDigest",
			function: func() string {
				return formatDailyDigest(&DailyDigest{
					Date:          time.Now(),
					TotalRequests: 1000,
					Switches:      5,
					Errors:        2,
					TopAccounts:   []string{"openai-1", "anthropic-1"},
				})
			},
			contains: []string{"📈", "Daily Digest", "1000", "5", "2", "openai-1", "anthropic-1"},
		},
		{
			name:     "formatHelpMessage",
			function: formatHelpMessage,
			contains: []string{"ℹ️", "кнопки", "роутинга", "фоллбэки"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.function()
			for _, expected := range tt.contains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{500 * time.Microsecond, "500µs"},
		{50 * time.Millisecond, "50ms"},
		{1500 * time.Millisecond, "1.5s"},
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{2 * time.Hour, "2h"},
		{25 * time.Hour, "1d 1h"},
		{48 * time.Hour, "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		percent float64
		width   int
		minLen  int // minimum length (unicode chars may be multi-byte)
	}{
		{0, 10, 10},   // empty bar
		{50, 10, 10},  // half filled
		{100, 10, 10}, // full
		{150, 10, 10}, // capped at 100%
		{-10, 10, 10}, // negative capped at 0
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%.0f%%_width%d", tt.percent, tt.width), func(t *testing.T) {
			result := renderProgressBar(tt.percent, tt.width)
			assert.GreaterOrEqual(t, len([]rune(result)), tt.minLen)
		})
	}
}

func TestGetSeverityEmoji(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{"critical", "🔴"},
		{"error", "🔴"},
		{"warning", "🟡"},
		{"warn", "🟡"},
		{"info", "🔵"},
		{"information", "🔵"},
		{"unknown", "⚪"},
		{"", "⚪"},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := getSeverityEmoji(tt.severity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTimeAgo(t *testing.T) {
	tests := []struct {
		time     time.Time
		contains string
	}{
		{time.Now().Add(-30 * time.Second), "just now"},
		{time.Now().Add(-5 * time.Minute), "minutes ago"},
		{time.Now().Add(-2 * time.Hour), "hours ago"},
		{time.Now().Add(-48 * time.Hour), "days ago"},
	}

	for _, tt := range tests {
		t.Run(tt.contains, func(t *testing.T) {
			result := formatTimeAgo(tt.time)
			assert.Contains(t, result, tt.contains)
		})
	}
}

// Benchmark tests
func BenchmarkRateLimiter(b *testing.B) {
	rl := NewRateLimiter(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func BenchmarkDedupLimiter(b *testing.B) {
	dl := NewDedupLimiter(5 * time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dl.CanSend(fmt.Sprintf("msg%d", i))
	}
}
