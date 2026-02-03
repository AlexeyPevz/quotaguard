#!/bin/bash
set -e

REPO_URL="${REPO_URL:-https://github.com/AlexeyPevz/quotaguard}"
BRANCH="${BRANCH:-main}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/quotaguard}"
CLIPROXY_AUTH_PATH="${CLIPROXY_AUTH_PATH:-/opt/cliproxyplus/auths}"

echo "QuotaGuard Agent Installation"
echo "============================="

# Step 1: Clone
echo "Cloning repository..."
git clone --branch "$BRANCH" --depth 1 "$REPO_URL" "$INSTALL_DIR"
cd "$INSTALL_DIR"

# Step 2: Detect OS/Arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
[ "$ARCH" = "x86_64" ] && ARCH="amd64" || ARCH="arm64"

# Step 3: Build
echo "Building for $OS-$ARCH..."
go build -o quotaguard ./cmd/quotaguard

# Step 4: Detect auths
echo "Scanning for CLIProxyAPI auths..."
AUTH_PATH=""
for path in "$CLIPROXY_AUTH_PATH" \
            "$HOME/.config/cliproxy/auths" \
            "$HOME/Library/Application Support/cliproxy/auths"; do
    if [ -d "$path" ] && [ "$(ls -A "$path" 2>/dev/null)" ]; then
        AUTH_PATH="$path"
        echo "Found: $path"
        break
    fi
done

# Step 5: Generate config
echo "Generating config..."
cat > config.yaml << EOF
version: "2.1"
server:
  host: "127.0.0.1"
  http_port: 8318
collector:
  mode: "hybrid"
telegram:
  enabled: true
  # Token set via /settoken command, stored in SQLite
metrics:
  enabled: true
middleware:
  enabled: true
  fail_open_timeout: "50ms"
EOF

# Step 6: Create data directory
mkdir -p data

echo ""
echo "============================="
echo "Installation complete"
echo ""
echo "1. Start: ./quotaguard serve --config config.yaml"
echo "2. Import accounts: ./quotaguard setup ${AUTH_PATH:-/path/to/auths}"
echo "3. Telegram: set telegram.enabled=true, then /settoken <bot_token>"
echo "4. Use /qg_status, /qg_fallback, /qg_thresholds, /qg_policy"
