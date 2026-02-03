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
1. В Telegram: `/settoken <bot_token>`
2. Команды: `/qg_status`, `/qg_thresholds`, `/qg_policy`, `/qg_fallback`
