# Quickstart (Beta, RU)

## 1. Предусловия

- Go 1.22+
- Доступ к SQLite файлу (локально)
- Папка CLIProxy auths (обычно `/opt/cliproxyplus/auths`)
- Telegram bot token (для UI)

## 2. Сборка

```bash
git clone https://github.com/AlexeyPevz/quotaguard.git
cd quotaguard
make build
```

## 3. Конфиг

Проверьте `config.yaml`:
- `server.http_port`
- `telegram.enabled: true`
- `telegram.bot_token`
- `router.ignore_estimated: true`

Рекомендуемые ENV:

```bash
export QUOTAGUARD_DB_PATH=./data/quotaguard.db
export QUOTAGUARD_CLIPROXY_AUTH_PATH=/opt/cliproxyplus/auths
export QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID=<client_id>
export QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET=<client_secret>
export QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID=<client_id>
export QUOTAGUARD_GEMINI_OAUTH_CLIENT_SECRET=<client_secret>
```

## 4. Запуск

```bash
./quotaguard serve --config config.yaml
```

Проверка:

```bash
curl -s http://127.0.0.1:8318/health
```

Ожидается `{"status":"healthy"...}`.

## 5. Telegram (standalone-режим)

1. Откройте бота.
2. Нажмите `/start`.
3. Используйте кнопки меню:
   - `Status`
   - `Routing`
   - `Accounts`
   - `Settings`
   - `Connect accounts`

Бот поддерживает и команды (`/qg_status`, `/qg_import` и т.д.), но в beta основной UX — кнопочный.

## 6. Проверка импорта и квот

1. В боте откройте `Status`.
2. В `Accounts` проверьте, что видны аккаунты из CLIProxy.
3. Убедитесь, что нет `config-only` аккаунтов.
4. Проверьте, что `gemini` помечен как estimated (если так настроено).

## 7. Проверка роутинга

1. В `Routing` проверьте policy/thresholds/fallback.
2. Подайте запросы через ваш прокси endpoint.
3. Убедитесь, что переключение идёт до полного исчерпания квоты.

## 8. Что считать успешным запуском beta

- Сервис healthy.
- Аккаунты импортируются автоматически.
- Квоты отображаются в Telegram.
- Роутинг переключает между рабочими аккаунтами.
- Alerts приходят при потере доступности аккаунта.
