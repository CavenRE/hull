#!/bin/bash
set -euo pipefail

APP_DIR="$HOME/.hull"
BIN_DIR="$HOME/.local/bin"
REPO_URL="https://github.com/CavenRE/hull.git"

echo "🚀 Installing Hull CLI..."

# 1. Environment Detection: Local vs Remote
if [ -f "./bin/hull" ] && [ -f "./setup/01-os-detect.sh" ]; then
    echo "ℹ Local development files detected. Installing from current directory..."
    
    if [ -d "$APP_DIR" ]; then
        rm -rf "$APP_DIR"
    fi
    
    mkdir -p "$APP_DIR"
    cp -a . "$APP_DIR/"
else
    if ! command -v git &> /dev/null; then
        echo "❌ Git is required to install from the web. Please install git and try again." >&2
        exit 1
    fi

    if [ -d "$APP_DIR" ]; then
        echo "ℹ Updating existing installation..."
        (cd "$APP_DIR" && git pull origin main --quiet)
    else
        echo "ℹ Cloning repository..."
        git clone --quiet "$REPO_URL" "$APP_DIR"
    fi
fi

# 2. Create the symlink
mkdir -p "$BIN_DIR"
ln -sf "$APP_DIR/bin/hull" "$BIN_DIR/hull"
chmod +x "$APP_DIR/bin/hull"

# 3. The Profile Injector
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
    echo "ℹ Adding $BIN_DIR to your PATH..."
    
    if [[ "${SHELL:-}" == *"zsh"* ]]; then
        PROFILE_FILE="$HOME/.zshrc"
    elif [[ "${SHELL:-}" == *"bash"* ]]; then
        if [ -f "$HOME/.bashrc" ]; then
            PROFILE_FILE="$HOME/.bashrc"
        else
            PROFILE_FILE="$HOME/.bash_profile"
        fi
    else
        PROFILE_FILE="$HOME/.profile"
    fi

    echo -e "\n# Added by Hull CLI\nexport PATH=\"\$PATH:$BIN_DIR\"" >> "$PROFILE_FILE"
    echo "✔ Added to $PROFILE_FILE. You may need to restart your terminal later."
fi

# 4. Hand off to the interactive wizard
echo "✔ Core installed successfully."
bash "$APP_DIR/setup/01-os-detect.sh"
