#!/bin/bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ui.sh"

step "Configuring Environment Variables"
ENV_FILE="$(dirname "${BASH_SOURCE[0]}")/../.env"

read -r -p "Enter project directory [default: $HOME/Work/Sites]: " SITES_DIR < /dev/tty
SITES_DIR=${SITES_DIR:-$HOME/Work/Sites}

if [ ! -d "$SITES_DIR" ]; then
    warning "Directory $SITES_DIR does not exist."
    read -r -p "Create it? (Y/n): " CREATE_DIR < /dev/tty
    if [[ "$CREATE_DIR" =~ ^[Yy]$ ]] || [[ -z "$CREATE_DIR" ]]; then
        mkdir -p "$SITES_DIR"
        success "Created $SITES_DIR"
    else
        fatal "Valid Sites directory required."
    fi
fi

read -r -p "Enter local TLD [default: test]: " TLD < /dev/tty
TLD=${TLD:-test}
TLD=${TLD#.}

find_free_port() {
    local port=$1
    while :; do
        if command -v ss >/dev/null 2>&1; then
            if ! ss -tuln | grep -q ":$port "; then
                echo "$port"
                return
            fi
        else
            if ! (echo > /dev/tcp/127.0.0.1/$port) >/dev/null 2>&1; then
                echo "$port"
                return
            fi
        fi
        port=$((port+1))
    done
}

info "Checking for available ports..."
HTTP_PORT=$(find_free_port 80)
HTTPS_PORT=$(find_free_port 443)

if [ "$HTTP_PORT" != "80" ] || [ "$HTTPS_PORT" != "443" ]; then
    warning "Standard ports are in use. Hull will bind to HTTP:$HTTP_PORT and HTTPS:$HTTPS_PORT."
    warning "You will need to append :$HTTPS_PORT to your .$TLD URLs (e.g. https://myapp.$TLD:$HTTPS_PORT)"
fi

echo "SITES_DIR=$SITES_DIR" > "$ENV_FILE"
echo "TLD=$TLD" >> "$ENV_FILE"
echo "HTTP_PORT=$HTTP_PORT" >> "$ENV_FILE"
echo "HTTPS_PORT=$HTTPS_PORT" >> "$ENV_FILE"
success "Configuration saved to .env"
source "$(dirname "${BASH_SOURCE[0]}")/04-caddy-router.sh"