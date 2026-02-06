# Runbook (Beta, RU)

## 1. Ежедневный operational-check

1. Health:
   - `curl -s http://127.0.0.1:8318/health`
2. Процесс:
   - `ps aux | rg quotaguard`
3. Логи:
   - `tail -n 200 logs/quotaguard.log` (или ваш stdout файл)
4. В боте:
   - `Status` и `Accounts` должны открываться без ошибок.

## 2. Стандартный запуск

```bash
export QUOTAGUARD_DB_PATH=./data/quotaguard.db
export QUOTAGUARD_CLIPROXY_AUTH_PATH=/opt/cliproxyplus/auths
export QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID=<client_id>
export QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET=<client_secret>
export QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID=<client_id>
export QUOTAGUARD_GEMINI_OAUTH_CLIENT_SECRET=<client_secret>
./quotaguard serve --config config.yaml
```

## 3. Graceful shutdown

- Отправьте `SIGTERM`.
- QuotaGuard корректно завершит HTTP, коллектор и работу с SQLite.

## 4. Инцидент: аккаунт выпал

Сигналы:
- Alert в Telegram вида `Account unavailable ... Re-login required`.
- Рост ошибок по конкретному provider/account.

Действия:
1. Откройте `Connect accounts` в Telegram.
2. Запустите логин нужного провайдера.
3. Отправьте callback URL в чат бота.
4. Проверьте, что аккаунт снова `Enabled` и участвует в роутинге.

## 5. Инцидент: Antigravity без групп

Симптом:
- По аккаунту есть only overall quota, без 3 групп.

Пояснение:
- Группы строятся из фактического ответа API.
- Если в ответе нет данных по части моделей, полная группировка невозможна.

Что сделать:
1. Перелогинить аккаунт через Telegram OAuth flow.
2. Проверить наличие нужных моделей в реальном model-list этого аккаунта.
3. Проверить логи коллекторов на 401/403/400.

## 6. Инцидент: Codex/Gemini не участвует в роутинге

Проверить:
- `router.ignore_estimated`.
- источник квоты (`estimated` или нет).
- `enabled` флаг аккаунта.

Рекомендация:
- В beta держать `ignore_estimated=true`.

## 7. Контроль настройки account checks

Через Telegram:
- `Settings` -> `Account checks`.
- Настройте `interval` и `timeout`.

Рекомендуемые значения:
- interval: `2-5m`
- timeout: `8-15s`

## 8. Обновление конфигурации без рестарта

Через Telegram:
- `Settings` -> `Reload`.

## 9. Recovery при проблемах БД

1. Остановить сервис.
2. Сделать backup `data/quotaguard.db`.
3. Проверить права на `data/`.
4. Запустить сервис снова.

## 10. Что не считать инцидентом в beta

- Gemini как estimated при `ignore_estimated=true`.
- Частичную группировку Antigravity, если API не вернул полные данные.
