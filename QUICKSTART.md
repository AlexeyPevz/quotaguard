# Quickstart (RU)

## 1. Установка
1. `git clone https://github.com/AlexeyPevz/quotaguard.git`
2. `cd quotaguard`
3. `make build`

## 2. Конфигурация
1. Откройте `config.yaml`
2. При необходимости измените порт и accounts

## 3. Запуск
1. `./quotaguard serve --config config.yaml`

## 4. Импорт аккаунтов
1. `./quotaguard setup /path/to/auths`

## 5. Telegram
### Вариант A — отдельный бот QuotaGuard
1. Создайте бота в BotFather и получите токен.
2. В `config.yaml`: `telegram.enabled: true`
3. В Telegram напишите этому боту: `/settoken <bot_token>`

### Вариант B — ваш существующий бот
1. Используйте `BotIntegrator` и прокидывайте updates в QuotaGuard.
2. В чате этого бота выполните: `/settoken <bot_token>`

Команды: `/qg_status`, `/qg_thresholds`, `/qg_policy`, `/qg_fallback`.
