#!/usr/bin/env bash
set -euo pipefail

REPO="paulbkim01/agent-profile-manager"
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

# Determine version to install
if [ -n "${APM_VERSION:-}" ]; then
    LATEST="$APM_VERSION"
    echo "Installing pinned version: ${LATEST}"
elif [ "${APM_CHANNEL:-stable}" = "beta" ]; then
    echo "Fetching latest pre-release..."
    LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
        | grep '"tag_name"' | head -1 \
        | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
else
    echo "Fetching latest stable release..."
    LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' | head -1 \
        | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
fi

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

# --- Shell integration ---
SHELL_NAME="$(basename "$SHELL" 2>/dev/null || true)"

add_shell_integration() {
    local shell_name="$1"
    local config_file="$2"
    local init_line="eval \"\$(apm init ${shell_name})\"  # Added by apm"

    if grep -q 'apm init' "$config_file" 2>/dev/null; then
        echo "Shell integration already present in ${config_file}"
        return
    fi

    echo "" >> "$config_file"
    echo "$init_line" >> "$config_file"
    echo "Added shell integration to ${config_file}"
    echo "  ${init_line}"
    echo ""
    echo "Restart your shell or run: source ${config_file}"
}

case "$SHELL_NAME" in
    bash)
        if [ "$OS" = "darwin" ] && [ ! -f "$HOME/.bashrc" ]; then
            SHELL_CONFIG="$HOME/.bash_profile"
        else
            SHELL_CONFIG="$HOME/.bashrc"
        fi
        add_shell_integration bash "$SHELL_CONFIG"
        ;;
    zsh)
        SHELL_CONFIG="$HOME/.zshrc"
        add_shell_integration zsh "$SHELL_CONFIG"
        ;;
    *)
        echo "Could not detect shell (got: ${SHELL_NAME:-unknown})."
        echo "Add the following line to your shell config manually:"
        echo ""
        echo "  eval \"\$(apm init <your-shell>)\""
        ;;
esac
