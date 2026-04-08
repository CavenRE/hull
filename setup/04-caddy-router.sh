#!/bin/bash
source "$(dirname "$0")/ui.sh"

step "Configuring Global Caddy Router"
CADDY_DIR="$(dirname "$0")/../system/caddy"

if ! docker network ls | grep -q caddy; then
    docker network create caddy
    success "Docker network 'caddy' created."
fi

cd "$CADDY_DIR" && docker compose up -d
success "Global router started."

info "Generating local SSL root CA. This may take 10 seconds."
sleep 10

ROOT_CERT_PATH="$CADDY_DIR/caddy-root.crt"

# FIX: Changed caddy-router to hull-router, and added error catching
if docker cp hull-router:/data/caddy/pki/authorities/local/root.crt "$ROOT_CERT_PATH" 2>/dev/null; then
    
    if [[ "$OS_FAMILY" == *"arch"* ]] || [[ "$OS" == "arch" ]]; then
        sudo cp "$ROOT_CERT_PATH" /etc/ca-certificates/trust-source/anchors/
        sudo update-ca-trust
    elif [[ "$OS_FAMILY" == *"debian"* ]] || [[ "$OS" == "ubuntu" ]] || [[ "$OS" == "debian" ]]; then
        sudo cp "$ROOT_CERT_PATH" /usr/local/share/ca-certificates/caddy-root.crt
        sudo update-ca-certificates
    fi

    mkdir -p "$HOME/.pki/nssdb"
    certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n "Hull Local Root" -i "$ROOT_CERT_PATH"
    
    success "SSL Root CA installed and trusted."
else
    fatal "Failed to extract SSL certificate from hull-router."
fi

bash "$(dirname "$0")/05-dns-resolver.sh"
