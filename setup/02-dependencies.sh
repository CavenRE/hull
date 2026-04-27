#!/bin/bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ui.sh"

step "Installing System Dependencies"

${UPDATE_CMD:-} > /dev/null 2>&1 || true

PACKAGES="curl git fzf jq nss"

if [[ "${OS_FAMILY:-}" == *"arch"* ]] || [[ "${OS:-}" == "arch" ]]; then
    PACKAGES="$PACKAGES dnsmasq docker docker-compose"
elif [[ "${OS_FAMILY:-}" == *"debian"* ]] || [[ "${OS:-}" == "ubuntu" ]] || [[ "${OS:-}" == "debian" ]]; then
    PACKAGES="$PACKAGES dnsmasq docker.io docker-compose-v2 libnss3-tools"
fi

info "Installing packages: $PACKAGES"
# shellcheck disable=SC2086
${PKG_MANAGER:-} $PACKAGES

sudo systemctl enable --now docker || true
sudo usermod -aG docker "$USER" || true

success "Dependencies installed."
source "$(dirname "${BASH_SOURCE[0]}")/03-environment.sh"