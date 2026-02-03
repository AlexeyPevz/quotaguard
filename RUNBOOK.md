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
2. Дискавери запускается при старте и по файловым событиям

## Типовые команды
- `./quotaguard setup /path/to/auths`
- `./quotaguard serve --config config.yaml`
- `/settoken <token>`
