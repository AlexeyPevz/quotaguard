# QuotaGuard

QuotaGuard — сервис маршрутизации запросов между провайдерами LLM с учётом квот, анти‑флаппинга и резервирований. Поддерживает автоматическое обнаружение аккаунтов CLIProxyAPI и управление через Telegram.

## Возможности
- Авто‑дискавери аккаунтов из CLIProxyAPI auths.
- Маршрутизация по политикам и порогам, анти‑флаппинг.
- Резервирования и учёт виртуального расхода.
- Управление через Telegram и интеграция с существующими ботами.
- Хранение динамических настроек в SQLite.
- Graceful shutdown.

## Быстрый старт
1. `make build`
2. `./quotaguard serve --config config.yaml`
3. `./quotaguard setup /path/to/auths`
4. В Telegram: `/settoken <bot_token>`

## CLI
- `quotaguard serve --config config.yaml` — запуск сервера.
- `quotaguard setup [auths_path]` — авто‑дискавери аккаунтов.
- `quotaguard quotas` — просмотр квот.
- `quotaguard check` — быстрый чек.

## Telegram: два режима

### Вариант A — отдельный бот QuotaGuard (проще)
1. Создайте бота через BotFather и получите токен.
2. В `config.yaml` включите:
   - `telegram.enabled: true`
3. Запустите QuotaGuard.
4. Напишите **этому боту** в любом чате:
   - `/settoken <TOKEN>`

QuotaGuard сохранит `token` и `chat_id` в SQLite и начнёт отвечать на команды.

### Вариант B — ваш существующий бот (интеграция)
Если у вас уже есть управляющий бот и вы не хотите второго:
1. Получите токен этого бота.
2. В вашем коде добавьте обработчик `HandleUpdate`:
   ```go
   tgClient, _ := tgbotapi.NewBotAPI(os.Getenv("USER_BOT_TOKEN"))
   qgBot := telegram.NewBot(os.Getenv("QG_BOT_TOKEN"), 0, true, &telegram.BotOptions{
       BotAPI:   nil, // используете свой polling
       Settings: settingsStore,
   })
   qgIntegrator := telegram.NewBotIntegrator(qgBot)

   updates := tgClient.GetUpdatesChan(tgbotapi.NewUpdate(0))
   for update := range updates {
       // ваши хендлеры
       qgIntegrator.HandleUpdate(update)
   }
   ```
3. В чате этого бота выполните:
   - `/settoken <TOKEN>`

### Команды
- `/qg_status` — статус системы.
- `/qg_fallback` — текущие fallback chains и обновление через JSON.
- `/qg_thresholds` — чтение/обновление порогов.
- `/qg_policy` — чтение/обновление политики.
- `/qg_codex_token <session_token>` — сохранить Codex session token.
- `/qg_codex_status` — проверить Codex auth.
- `/qg_antigravity_status` — статус авто‑детекта Antigravity.
- `/qg_import` — импорт аккаунтов.
- `/qg_export` — экспорт `config.yaml`.
- `/qg_reload` — перезагрузка конфигурации.
- `/settoken <token>` — сохранить токен и chat_id в SQLite.

## Переменные окружения
- `QUOTAGUARD_CONFIG_PATH` — путь к `config.yaml`.
- `QUOTAGUARD_DB_PATH` — путь к SQLite БД.
- `QUOTAGUARD_CLIPROXY_AUTH_PATH` — путь к папке auths.
- `QUOTAGUARD_ANTIGRAVITY_PORT` — порт Antigravity сервера.
- `QUOTAGUARD_ANTIGRAVITY_CSRF` — CSRF токен Antigravity.
- `QUOTAGUARD_ANTIGRAVITY_START_CMD` — команда авто‑запуска IDE/сервера.
- `QUOTAGUARD_ANTIGRAVITY_START_TIMEOUT` — сколько ждать запуска (по умолчанию `15s`).
- Если `QUOTAGUARD_ANTIGRAVITY_START_CMD` не задана, QuotaGuard попробует запустить `antigravity` из `PATH`.
- `QUOTAGUARD_GOOGLE_CLIENT_ID` / `QUOTAGUARD_GOOGLE_CLIENT_SECRET` — OAuth client для Antigravity (refresh_token).
- `QUOTAGUARD_UTLS=1` — включить uTLS (имитация браузерного TLS отпечатка).
- `QUOTAGUARD_COLLECTOR_WORKERS` — воркеры активного коллектора (по умолчанию `8`).
- `QUOTAGUARD_COLLECTOR_JITTER` — jitter между запросами (например `250ms`).
- `SHUTDOWN_TIMEOUT` — таймаут graceful shutdown.

## Docker
1. `docker compose -f docker-compose.yml up -d`
2. Отредактировать `config.yaml` и передать токен через `/settoken`

## Структура репозитория
- `cmd/quotaguard` — CLI.
- `internal/api` — HTTP API.
- `internal/router` — маршрутизация.
- `internal/collector` — сбор квот.
- `internal/telegram` — Telegram бот и интеграция.
- `internal/cliproxy` — авто‑дискавери аккаунтов.
- `internal/store` — SQLite + settings.

## Документация
- `QUICKSTART.md`
- `RUNBOOK.md`
- `.github/workflows/ci.yml` — CI для Go
- `TELEGRAM.md` — сценарии интеграции Telegram
- `AGENTS.md` — агентная установка
