
#!/usr/bin/env bash
set -euo pipefail

REPO="jackykornet/sshctl"
INSTALL_DIR="/usr/local/bin"

echo "Installing sshctl..."

# Check dependencies
for cmd in curl tar; do
    command -v "$cmd" >/dev/null 2>&1 || {
        echo "Missing dependency: $cmd"
        exit 1
    }
done

# Detect architecture
case "$(uname -m)" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: $(uname -m)"
        exit 1
        ;;
esac

# Get latest release
API="https://api.github.com/repos/${REPO}/releases/latest"

DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/sshctl_linux_${ARCH}.tar.gz"

# Use the actual asset naming convention
DOWNLOAD_URL=$(curl -fsSL "$API" |
    grep "browser_download_url" |
    grep "linux_${ARCH}.tar.gz" |
    head -n 1 |
    cut -d '"' -f 4)

if [[ -z "$DOWNLOAD_URL" ]]; then
    echo "No Linux release found for ${ARCH}"
    exit 1
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading $DOWNLOAD_URL"

curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/sshctl.tar.gz"

tar -xzf "$TMP_DIR/sshctl.tar.gz" -C "$TMP_DIR"

# Locate binary
BINARY=$(find "$TMP_DIR" -type f -name "sshctl" | head -n 1)

if [[ -z "$BINARY" ]]; then
    echo "Could not find sshctl binary in release archive"
    exit 1
fi

# Install
sudo install -m 755 "$BINARY" "$INSTALL_DIR/sshctl"

echo ""
echo "sshctl installed successfully!"
echo "Location: $INSTALL_DIR/sshctl"
echo ""
echo "Run: sshctl --help"