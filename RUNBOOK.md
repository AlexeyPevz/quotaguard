# Runbook (RU)

## Запуск в проде
1. Проверьте `config.yaml`
2. Установите `QUOTAGUARD_DB_PATH`
3. Запустите `./quotaguard serve --config config.yaml`

## Graceful shutdown
1. Отправьте `SIGTERM`
2. Сервис завершит HTTP, освободит резервации, сбросит коллектор, закроет БД

## Восстановление
1. Проверьте доступность SQLite файла
2. Проверьте права на директорию `data/`

## Авто‑дискавери
1. Путь задаётся через `QUOTAGUARD_CLIPROXY_AUTH_PATH`
2. Дискавери запускается автоматически при старте и по файловым событиям
3. Команда `./quotaguard setup` нужна только для ручного форс‑импорта

## Antigravity (local language server)
Для автоматического сбора квот нужны:
- `QUOTAGUARD_ANTIGRAVITY_PORT`
- `QUOTAGUARD_ANTIGRAVITY_CSRF`

QuotaGuard попробует авто‑детект из процесса, но если не найдёт — задайте вручную.

Авто‑запуск (best effort):
- `QUOTAGUARD_ANTIGRAVITY_START_CMD` — команда запуска IDE/языкового сервера
- `QUOTAGUARD_ANTIGRAVITY_START_TIMEOUT` — сколько ждать появления сервера (по умолчанию `15s`)

Если `QUOTAGUARD_ANTIGRAVITY_START_CMD` не задана, QuotaGuard попробует стартовать через `antigravity` из `PATH`.
Если сервер не стартует сам, укажите корректную команду запуска IDE или откройте IDE вручную.

## Antigravity (Cloud Code OAuth)
Для прямого облачного запроса нужны:
- `refresh_token` в SQLite (импортируется через `setup`)
- OAuth client:
  - `QUOTAGUARD_GOOGLE_CLIENT_ID`
  - `QUOTAGUARD_GOOGLE_CLIENT_SECRET` (если требуется)

## Активный коллектор
- `QUOTAGUARD_COLLECTOR_WORKERS` — количество воркеров (по умолчанию `8`)
- `QUOTAGUARD_COLLECTOR_JITTER` — jitter между запросами (например `250ms`)
- `QUOTAGUARD_UTLS=1` — включить uTLS

## Codex (ChatGPT)
Если аккаунт Codex не имеет `session_token` в auth‑файле, можно задать токен через Telegram:
- `/qg_codex_token <session_token>`
- Проверка: `/qg_codex_status`

## Типовые команды
- `./quotaguard setup /path/to/auths`
- `./quotaguard serve --config config.yaml`
- `/settoken <token>`
