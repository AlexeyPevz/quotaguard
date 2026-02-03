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

## Telegram команды
- `/qg_status` — статус системы.
- `/qg_fallback` — текущие fallback chains и обновление через JSON.
- `/qg_thresholds` — чтение/обновление порогов.
- `/qg_policy` — чтение/обновление политики.
- `/qg_import` — импорт аккаунтов.
- `/qg_export` — экспорт `config.yaml`.
- `/qg_reload` — перезагрузка конфигурации.
- `/settoken <token>` — сохранить токен бота в SQLite.

## Переменные окружения
- `QUOTAGUARD_CONFIG_PATH` — путь к `config.yaml`.
- `QUOTAGUARD_DB_PATH` — путь к SQLite БД.
- `QUOTAGUARD_CLIPROXY_AUTH_PATH` — путь к папке auths.
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
