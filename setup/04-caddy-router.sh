#!/bin/bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ui.sh"

step "Configuring Global Caddy Router"
CADDY_DIR="$(dirname "${BASH_SOURCE[0]}")/../system/caddy"

set -a
source "$(dirname "${BASH_SOURCE[0]}")/../.env"
set +a

if ! docker network ls | grep -q caddy; then
    docker network create caddy || true
    success "Docker network 'caddy' created."
fi

cd "$CADDY_DIR"
docker compose up -d
success "Global router started."
info "Generating local SSL root CA. Waiting for Caddy to generate..."

ROOT_CERT_PATH="$CADDY_DIR/caddy-root.crt"
MAX_RETRIES=30
RETRY_COUNT=0
CERT_READY=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker cp hull-router:/data/caddy/pki/authorities/local/root.crt "$ROOT_CERT_PATH" 2>/dev/null; then
        CERT_READY=true
        break
    fi
    sleep 1
    RETRY_COUNT=$((RETRY_COUNT+1))
done

if [ "$CERT_READY" = true ]; then

    if [[ "${OS_FAMILY:-}" == *"arch"* ]] || [[ "${OS:-}" == "arch" ]]; then
        sudo cp "$ROOT_CERT_PATH" /etc/ca-certificates/trust-source/anchors/
        sudo update-ca-trust || true
    elif [[ "${OS_FAMILY:-}" == *"debian"* ]] || [[ "${OS:-}" == "ubuntu" ]] || [[ "${OS:-}" == "debian" ]]; then
        sudo cp "$ROOT_CERT_PATH" /usr/local/share/ca-certificates/caddy-root.crt
        sudo update-ca-certificates || true
    fi

    mkdir -p "$HOME/.pki/nssdb"
    if command -v certutil &> /dev/null; then
        certutil -d sql:"$HOME/.pki/nssdb" -A -t "C,," -n "Hull Local Root" -i "$ROOT_CERT_PATH" || true
    fi

    success "SSL Root CA installed and trusted."
else
    fatal "Failed to extract SSL certificate from hull-router."
fi

source "$(dirname "${BASH_SOURCE[0]}")/05-dns-resolver.sh"