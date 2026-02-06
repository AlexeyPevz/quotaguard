# Telegram Integration (Beta, RU)

## 1. Режимы работы

### Режим A: standalone QuotaGuard bot (рекомендуется)

Используется полный UX:
- кнопочные меню,
- inline actions,
- управление аккаунтами,
- login flow через кнопки.

### Режим B: интеграция в существующий бот

Используется `BotIntegrator`.

Важно для beta:
- команды `/qg_*` работают,
- `/settoken` работает,
- полный callback UX ограничен реализацией вашего update-loop и текущим интегратором.

## 2. Standalone setup

В `config.yaml`:
- `telegram.enabled: true`
- `telegram.bot_token: <BOT_TOKEN>`

В ENV для Telegram-driven OAuth:
- `QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID`
- `QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET`
- `QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID`
- `QUOTAGUARD_GEMINI_OAUTH_CLIENT_SECRET`

Запуск:

```bash
./quotaguard serve --config config.yaml
```

В чате:
- `/start`

## 3. Основные экранные разделы (standalone)

- `Status` — сводка здоровья, квоты, активные аккаунты.
- `Routing` — policy/thresholds/fallback/ignore_estimated.
- `Accounts` — список routable аккаунтов, enable/disable.
- `Settings` — reload/import/export/account checks.
- `Connect accounts` — OAuth login поток в Telegram.

## 4. OAuth login прямо из Telegram

Поток:
1. Выбрать provider в `Connect accounts`.
2. Нажать кнопку открытия OAuth URL.
3. После редиректа отправить полный callback URL в чат.
4. Бот завершит exchange и создаст/обновит account+credentials.

Поддерживаемые провайдеры для этого потока:
- `antigravity`
- `gemini`

## 5. Интеграция в существующий бот (командный режим)

Пример:

```go
updates := tgClient.GetUpdatesChan(tgbotapi.NewUpdate(0))
qgIntegrator := telegram.NewBotIntegrator(qgBot)

for update := range updates {
    // ваши обработчики
    qgIntegrator.HandleUpdate(update)
}
```

Команды:
- `/qg_status`
- `/qg_thresholds`
- `/qg_policy`
- `/qg_fallback`
- `/qg_alerts`
- `/qg_import`
- `/qg_export`
- `/qg_reload`
- `/qg_codex_token`
- `/qg_codex_status`
- `/qg_antigravity_status`
- `/settoken`

## 6. UX и форматирование

В standalone-боте:
- провайдеры и аккаунты сгруппированы,
- прогресс-бары вынесены под заголовок аккаунта,
- цвета и текстовые статусы показывают warning/critical,
- для аккаунтов показываются time-to-reset, last-call, active marker (если есть данные).

## 7. Безопасность

- Не отправляйте long-lived токены в публичные чаты.
- Используйте приватный админ-чат.
- Ограничьте доступ к вашему боту только доверенным пользователям.
