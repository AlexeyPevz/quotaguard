# AGENTS.md (Beta, RU)

Документ для агентной установки и smoke-проверки QuotaGuard.

## 1. Цель

Развернуть рабочий beta-инстанс QuotaGuard, который:
- автоподхватывает аккаунты из CLIProxy auths,
- показывает квоты в Telegram,
- маршрутизирует запросы по квотам и fallback.

## 2. Быстрая установка

```bash
curl -fsSL https://raw.githubusercontent.com/AlexeyPevz/quotaguard/main/agent-install.sh | bash
```

Или вручную:

```bash
git clone https://github.com/AlexeyPevz/quotaguard.git
cd quotaguard
make build
```

## 3. Обязательные переменные

```bash
export QUOTAGUARD_DB_PATH=./data/quotaguard.db
export QUOTAGUARD_CLIPROXY_AUTH_PATH=/opt/cliproxyplus/auths
export QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_ID=<client_id>
export QUOTAGUARD_ANTIGRAVITY_OAUTH_CLIENT_SECRET=<client_secret>
export QUOTAGUARD_GEMINI_OAUTH_CLIENT_ID=<client_id>
export QUOTAGUARD_GEMINI_OAUTH_CLIENT_SECRET=<client_secret>
```

Опционально:

```bash
export QUOTAGUARD_IGNORE_ESTIMATED=true
export QUOTAGUARD_COLLECTOR_WORKERS=8
export QUOTAGUARD_COLLECTOR_JITTER=250ms
```

## 4. Запуск

```bash
./quotaguard serve --config config.yaml
```

## 5. Smoke checklist (agent must verify)

1. Health endpoint:
   - `curl -s http://127.0.0.1:8318/health`
2. Auto-import:
   - в логах есть импорт аккаунтов из CLIProxy
3. Telegram:
   - бот отвечает на `/start` и/или `/qg_status`
4. /quota semantics:
   - нет config-only аккаунтов
   - есть только routable/imported аккаунты
5. Router behavior:
   - `ignore_estimated=true` не пускает estimated аккаунты в selection

## 6. Проверка Antigravity

Ожидание:
- квота есть минимум overall,
- по возможности есть 3 группы.

Если групп нет у отдельного аккаунта:
- это допустимо для beta, если API не вернул group-данные.

## 7. Проверка Codex

Ожидание:
- аккаунты codex импортированы из CLIProxy auths,
- квота обновляется без ручного token-set в большинстве случаев.

## 7.1 Проверка Claude Code

Ожидание:
- аккаунты `claude` / `claude-code` импортируются из CLIProxy auths,
- в beta они могут отображаться как `estimated` (это допустимо).

## 8. Telegram login flow

Для новых аккаунтов:
1. `Connect accounts` в standalone-боте.
2. OAuth URL.
3. callback URL в чат.
4. Проверка, что account появился в `Accounts`.

## 9. Интеграция в существующий бот

Если нужен single-bot режим:
- использовать `BotIntegrator`,
- гарантировать проброс updates в `HandleUpdate`.

Для beta рекомендован standalone-режим, если нужен полный кнопочный UX.

## 10. Что агент не должен делать

- Не включать `ignore_estimated=false` без явного требования.
- Не удалять существующие аккаунты/креды без бэкапа.
- Не принимать отсутствие групп Antigravity за критический падеж, если overall quota присутствует.
