# QuotaGuard (Beta)

QuotaGuard — маршрутизатор LLM-запросов по квотам с авто-импортом аккаунтов из CLIProxy и управлением через Telegram.

## Статус Beta

Текущий релиз — `beta`.

Готово и работает:
- Авто-дискавери аккаунтов из CLIProxy auths.
- Сбор квот `codex`, `antigravity`, `gemini` (gemini в режиме estimated).
- Роутинг с порогами, fallback-цепочками, анти-флаппингом.
- Исключение `estimated` аккаунтов из роутинга (`router.ignore_estimated: true`).
- Telegram UX для standalone-бота: меню, кнопки, действия по аккаунтам.

Ограничения beta:
- `gemini` сейчас показывается как estimated и не участвует в роутинге при `ignore_estimated=true`.
- Разбиение Antigravity на группы зависит от фактического ответа облака для конкретного аккаунта.
- Интеграция в существующий бот поддерживает команды `qg_*`; полный callback-UI доступен в standalone-режиме.

## Архитектура

- `internal/collector` — активный сбор квот.
- `internal/router` — выбор аккаунта и fallback.
- `internal/cliproxy` — обнаружение/импорт auth-файлов.
- `internal/store` — SQLite, динамические настройки.
- `internal/telegram` — standalone-бот и интеграция в чужой бот.

## Установка

```bash
make build
./quotaguard serve --config config.yaml
```

Минимальные ENV:
- `QUOTAGUARD_DB_PATH` (например `./data/quotaguard.db`)
- `QUOTAGUARD_CLIPROXY_AUTH_PATH` (обычно `/opt/cliproxyplus/auths`)

## Первый запуск

1. Запустите сервис:
   - `./quotaguard serve --config config.yaml`
2. Проверьте health:
   - `curl -s http://127.0.0.1:8318/health`
3. В Telegram откройте бота и выполните `/start`.
4. Нажмите `Обновить` в меню Accounts/Status, убедитесь, что аккаунты импортированы.

## Telegram режимы

### Режим A: standalone-бот (рекомендуется)

В `config.yaml`:
- `telegram.enabled: true`
- `telegram.bot_token: <BOT_TOKEN>`

Что доступно:
- Кнопочное меню (status/routing/settings/accounts/login).
- Временное отключение аккаунтов от роутинга.
- Настройка порогов/политики/fallback.
- Логин новых аккаунтов через OAuth URL + callback в чат.

### Режим B: встраивание в существующий бот

Используйте `BotIntegrator` из `internal/telegram/integration.go`.

Важно:
- Поддерживаются команды `/qg_*` и `/settoken`.
- Full inline callback UX (кнопки) в интеграторе сейчас ограничен.

## /quota и отображение

В `/quota` попадают только аккаунты, у которых:
- `provider_type != ""`
- `credentials_ref != ""`
- `enabled = true`

`config-only` аккаунты (пример: `openai-primary`) скрыты.

## Роутинг

Ключевые параметры `router` в `config.yaml`:
- `thresholds.warning`
- `thresholds.switch`
- `thresholds.critical`
- `fallback_chains`
- `ignore_estimated` (рекомендуется `true`)

Логика:
- до критики переключаем заранее на более безопасный аккаунт,
- если все близко к исчерпанию, дожимаем доступные,
- при проблемах аккаунтов уходит alert в Telegram.

## Переменные окружения

- `QUOTAGUARD_CONFIG_PATH`
- `QUOTAGUARD_DB_PATH`
- `QUOTAGUARD_CLIPROXY_AUTH_PATH`
- `QUOTAGUARD_IGNORE_ESTIMATED`
- `QUOTAGUARD_COLLECTOR_WORKERS`
- `QUOTAGUARD_COLLECTOR_JITTER`
- `QUOTAGUARD_UTLS=1`
- `QUOTAGUARD_ACCOUNT_CHECK_INTERVAL`
- `QUOTAGUARD_ACCOUNT_CHECK_TIMEOUT`
- `QUOTAGUARD_GOOGLE_CLIENT_ID`
- `QUOTAGUARD_GOOGLE_CLIENT_SECRET`
- `QUOTAGUARD_GOOGLE_CLIENT_SECRET_CANDIDATES` (через запятую, опционально)
- `QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID`
- `QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET`
- `QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID`
- `QUOTAGUARD_GEMINI_OAUTH_CLIENT_SECRET`
- `GEMINI_OAUTH_PATH`
- `QWEN_OAUTH_PATH`

## Команды CLI

- `./quotaguard serve --config config.yaml`
- `./quotaguard setup /path/to/auths`
- `./quotaguard quotas`
- `./quotaguard check`

## Документация

- `QUICKSTART.md` — быстрый путь для людей.
- `RUNBOOK.md` — эксплуатация/инциденты.
- `TELEGRAM.md` — UX и интеграция Telegram.
- `AGENTS.md` — установка и проверка для агентов.
