
#!/usr/bin/env bash
set -euo pipefail

REPO="jackykornet/sshctl"
INSTALL_DIR="/usr/local/bin"

echo "Installing sshctl..."

# Check dependencies
for cmd in curl tar; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "Missing dependency: $cmd"
        exit 1
    fi
done

# Detect architecture
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64)
        ARCH="x86_64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Get latest release information
API="https://api.github.com/repos/${REPO}/releases/latest"

DOWNLOAD_URL=$(curl -fsSL "$API" |
    grep "browser_download_url" |
    grep "linux-${ARCH}.tar.gz" |
    head -n 1 |
    cut -d '"' -f 4)

if [[ -z "$DOWNLOAD_URL" ]]; then
    echo "Could not find a Linux release for ${ARCH}"
    exit 1
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading: $DOWNLOAD_URL"

curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/sshctl.tar.gz"

tar -xzf "$TMP_DIR/sshctl.tar.gz" -C "$TMP_DIR"

# Install binaries
for BIN in sshctl sshctl-tui; do
    FILE=$(find "$TMP_DIR" -type f -name "$BIN" | head -n 1)

    if [[ -z "$FILE" ]]; then
        echo "Could not find binary: $BIN"
        exit 1
    fi

    sudo install -m 755 "$FILE" "$INSTALL_DIR/$BIN"
    echo "Installed $BIN"
done

echo ""
echo "sshctl installation completed!"
echo "Run: sshctl-tui"