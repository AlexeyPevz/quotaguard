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
- `QUOTAGUARD_PUBLIC_BASE_URL` (опционально, для callback relay без SSH tunnel)

Google OAuth redirect URI (важно для `antigravity`):
- если используете public relay (`QUOTAGUARD_PUBLIC_BASE_URL`), в Google Console добавьте:
  - `<QUOTAGUARD_PUBLIC_BASE_URL>/oauth/callback/antigravity`
- если relay не используете, redirect URI по умолчанию:
  - `http://localhost:1456/oauth-callback`

Примечание: URI должен совпадать *точно*, без динамических query-параметров.

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

## 4. Login прямо из Telegram

Поток:
1. Выбрать provider в `Connect accounts`.
2. Следовать шагам для выбранного провайдера.
3. После завершения бот автоматически создаст/обновит account+credentials и отправит подтверждение в чат.

### `antigravity` / `gemini`

1. Нажать кнопку открытия OAuth URL.
2. Пройти login в браузере.
3. Бот автоматически импортирует auth после callback.

Примечание:
- client_id/client_secret можно задать через ENV,
- если ENV отсутствуют, QuotaGuard пытается взять их из локальных auth JSON (`QUOTAGUARD_CLIPROXY_AUTH_PATH`, `/opt/cliproxyplus/auths`, `~/.config/gcloud/...`).
- при заданном `QUOTAGUARD_PUBLIC_BASE_URL` callback идёт через QuotaGuard API (`/oauth/callback/:provider`) и SSH tunnel обычно не нужен.
- без `QUOTAGUARD_PUBLIC_BASE_URL` для localhost-callback на другом устройстве может понадобиться SSH tunnel (зависит от provider flow).

### `codex`

1. Бот запускает device-auth на хосте QuotaGuard.
2. В чате приходит ссылка и one-time code.
3. После авторизации в браузере QuotaGuard автоматически импортирует токены из `~/.codex/auth.json`.
4. В чат приходит подтверждение подключения.

### `claude` / `claude-code`

Подключение идёт через встроенный `cliproxyapi` OAuth flow (`-claude-login`) с авто-импортом результата.

### `qwen`

Подключение идёт через встроенный `cliproxyapi` OAuth flow (`-qwen-login`) с авто-импортом результата.

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
