#!/usr/bin/env bash
set -euo pipefail

REPO="paulbkim/agent-profile-manager"
BINARY_NAME="apm"
INSTALL_DIR="${APM_INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    darwin|linux) ;;
    *) echo "Error: unsupported OS: $OS" >&2; exit 1 ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Error: unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

echo "Detected: ${OS}/${ARCH}"

# Get latest release tag
echo "Fetching latest release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
if [ -z "$LATEST" ]; then
    echo "Error: could not determine latest release" >&2
    exit 1
fi
echo "Latest release: ${LATEST}"

# Download binary
ASSET_NAME="${BINARY_NAME}-${OS}-${ARCH}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${ASSET_NAME}"

TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

echo "Downloading ${DOWNLOAD_URL}..."
if ! curl -fsSL -o "$TMPFILE" "$DOWNLOAD_URL"; then
    echo "Error: failed to download ${DOWNLOAD_URL}" >&2
    echo "Check that a release exists with asset: ${ASSET_NAME}" >&2
    exit 1
fi

chmod +x "$TMPFILE"

# Install
TARGET="${INSTALL_DIR}/${BINARY_NAME}"
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMPFILE" "$TARGET"
else
    echo "Installing to ${INSTALL_DIR} requires elevated permissions."
    sudo mv "$TMPFILE" "$TARGET"
fi

echo "Installed ${BINARY_NAME} ${LATEST} to ${TARGET}"
echo ""
echo "Run 'apm init' to get started."
