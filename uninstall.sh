#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="apm"

# Find the installed binary
BINARY_PATH=$(command -v "$BINARY_NAME" 2>/dev/null || true)

if [ -z "$BINARY_PATH" ]; then
    echo "Error: ${BINARY_NAME} is not installed (not found in PATH)" >&2
    exit 1
fi

echo "Found ${BINARY_NAME} at: ${BINARY_PATH}"

# Remove binary
if [ -w "$(dirname "$BINARY_PATH")" ]; then
    rm "$BINARY_PATH"
else
    echo "Removing ${BINARY_PATH} requires elevated permissions."
    sudo rm "$BINARY_PATH"
fi

echo "Removed ${BINARY_PATH}"

# Offer to clean up config
APM_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/apm"
if [ -d "$APM_DIR" ]; then
    printf "Remove APM config directory (%s)? [y/N] " "$APM_DIR"
    read -r answer
    if [ "$answer" = "y" ] || [ "$answer" = "Y" ]; then
        rm -rf "$APM_DIR"
        echo "Removed ${APM_DIR}"
    else
        echo "Config directory preserved at ${APM_DIR}"
    fi
fi

echo "Uninstall complete."
