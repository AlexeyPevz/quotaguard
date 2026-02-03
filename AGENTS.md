## AGENTS.md

Этот файл описывает быстрый сценарий для агентной установки QuotaGuard.

### Репозиторий
- `https://github.com/AlexeyPevz/quotaguard`

### Быстрая установка (one‑liner)
```bash
curl -fsSL https://raw.githubusercontent.com/AlexeyPevz/quotaguard/main/agent-install.sh | bash
```

### Параметры установки
Можно переопределить:
- `REPO_URL` (по умолчанию репозиторий выше)
- `BRANCH` (по умолчанию `main`)
- `INSTALL_DIR` (по умолчанию `~/quotaguard`)
- `CLIPROXY_AUTH_PATH` (по умолчанию `/opt/cliproxyplus/auths`)

Пример:
```bash
REPO_URL=https://github.com/AlexeyPevz/quotaguard \
BRANCH=main \
INSTALL_DIR=/opt/quotaguard \
CLIPROXY_AUTH_PATH=/opt/cliproxyplus/auths \
bash agent-install.sh
```

### Автонастройка
После установки:
1. Запуск: `./quotaguard serve --config config.yaml`
2. Импорт аккаунтов: `./quotaguard setup /path/to/auths`
3. Telegram:
   - Включите `telegram.enabled: true`
   - `/settoken <TOKEN>`
