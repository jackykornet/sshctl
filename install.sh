
#!/usr/bin/env bash
set -euo pipefail

REPO="jackykornet/sshctl"
INSTALL_DIR="/usr/local/bin"

echo "Installing sshctl..."

# Detect architecture
case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $(uname -m)"
        exit 1
        ;;
esac

# Get latest release download URL
API="https://api.github.com/repos/${REPO}/releases/latest"

DOWNLOAD_URL=$(curl -fsSL "$API" |
    grep "browser_download_url" |
    grep "linux_${ARCH}.tar.gz" |
    head -n 1 |
    cut -d '"' -f 4)

if [[ -z "$DOWNLOAD_URL" ]]; then
    echo "No release found for linux_${ARCH}"
    exit 1
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading $DOWNLOAD_URL"

curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/sshctl.tar.gz"

# Extract archive
tar -xzf "$TMP_DIR/sshctl.tar.gz" -C "$TMP_DIR"

# Find the versioned executable
BINARY=$(find "$TMP_DIR" -maxdepth 1 -type f \
    -name "sshctl_*_linux_${ARCH}" | head -n 1)

if [[ -z "$BINARY" ]]; then
    echo "Executable not found in archive!"
    exit 1
fi

# Install executable
sudo install -m 755 "$BINARY" "$INSTALL_DIR/sshctl"

echo ""
echo "================================"
echo "sshctl installed successfully!"
echo "================================"
echo ""
echo "Location: $INSTALL_DIR/sshctl"
echo ""
echo "Run: sshctl --help"
