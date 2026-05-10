#!/bin/sh
set -e

REPO="youngwoocho02/unity-scanner"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux)  ;;
  darwin) ;;
  *)      echo "Unsupported OS: $OS (use Windows instructions in README)"; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)   ARCH="amd64" ;;
  aarch64|arm64)  ARCH="arm64" ;;
  *)              echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

INSTALL_DIR="$HOME/.local/bin"
mkdir -p "$INSTALL_DIR"

URL="https://github.com/${REPO}/releases/latest/download/unity-scanner-${OS}-${ARCH}"

echo "Downloading unity-scanner for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o "$INSTALL_DIR/unity-scanner"
chmod +x "$INSTALL_DIR/unity-scanner"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    export PATH="$INSTALL_DIR:$PATH"
    LINE="export PATH=\"$INSTALL_DIR:\$PATH\""
    SHELL_NAME="$(basename "$SHELL")"
    case "$SHELL_NAME" in
      zsh)  RC_FILE="$HOME/.zshrc" ;;
      bash) RC_FILE="$HOME/.bashrc" ;;
      *)    RC_FILE="$HOME/.profile" ;;
    esac
    touch "$RC_FILE"
    echo "$LINE" >> "$RC_FILE"
    echo "Added $INSTALL_DIR to PATH (restart shell to apply)" ;;
esac

echo "Installed unity-scanner to $INSTALL_DIR/unity-scanner"
"$INSTALL_DIR/unity-scanner" version
