# Telegram Integration (RU)

Этот документ описывает два сценария работы Telegram‑интерфейса QuotaGuard.

## Вариант A — отдельный бот QuotaGuard
Подходит большинству пользователей.

1. Создайте бота через BotFather и получите токен.
2. В `config.yaml` включите:
   - `telegram.enabled: true`
3. Запустите QuotaGuard.
4. Напишите **этому боту**:
   - `/settoken <TOKEN>`

После этого QuotaGuard сохранит `token` и `chat_id` в SQLite и начнёт отвечать на команды `/qg_*`.

## Вариант B — интеграция с вашим существующим ботом
Если у вас уже есть управляющий бот и вы не хотите второго.

1. Используйте `BotIntegrator` из `internal/telegram/integration.go`.
2. Передавайте updates QuotaGuard‑интегратору.
3. В чате бота выполните:
   - `/settoken <TOKEN>`

Пример (Go):
```go
tgClient, _ := tgbotapi.NewBotAPI(os.Getenv("USER_BOT_TOKEN"))

qgBot := telegram.NewBot(os.Getenv("QG_BOT_TOKEN"), 0, true, &telegram.BotOptions{
    BotAPI:   nil, // ваш polling
    Settings: settingsStore,
})
qgIntegrator := telegram.NewBotIntegrator(qgBot)

updates := tgClient.GetUpdatesChan(tgbotapi.NewUpdate(0))
for update := range updates {
    // ваши хендлеры
    qgIntegrator.HandleUpdate(update)
}
```

## Команды
- `/qg_status` — статус системы.
- `/qg_fallback` — fallback chains (JSON).
- `/qg_thresholds` — пороги.
- `/qg_policy` — политика маршрутизации.
- `/qg_import` — импорт аккаунтов.
- `/qg_export` — экспорт `config.yaml`.
- `/qg_reload` — перезагрузка конфигурации.
- `/settoken <token>` — сохранить токен и chat_id.
