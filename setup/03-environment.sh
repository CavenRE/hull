#!/bin/bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ui.sh"

step "Configuring Environment Variables"
ENV_FILE="$(dirname "${BASH_SOURCE[0]}")/../.env"

read -r -p "Enter project directory [default: $HOME/Work/Sites]: " SITES_DIR
SITES_DIR=${SITES_DIR:-$HOME/Work/Sites}

if [ ! -d "$SITES_DIR" ]; then
    warning "Directory $SITES_DIR does not exist."
    read -r -p "Create it? (Y/n): " CREATE_DIR
    if [[ "$CREATE_DIR" =~ ^[Yy]$ ]] || [[ -z "$CREATE_DIR" ]]; then
        mkdir -p "$SITES_DIR"
        success "Created $SITES_DIR"
    else
        fatal "Valid Sites directory required."
    fi
fi

read -r -p "Enter local TLD [default: test]: " TLD
TLD=${TLD:-test}
TLD=${TLD#.}

echo "SITES_DIR=$SITES_DIR" > "$ENV_FILE"
echo "TLD=$TLD" >> "$ENV_FILE"

success "Configuration saved to .env"
source "$(dirname "${BASH_SOURCE[0]}")/04-caddy-router.sh"