#!/bin/bash
source "$(dirname "$0")/ui.sh"

step "Installing System Dependencies"

$UPDATE_CMD > /dev/null 2>&1

PACKAGES="curl git fzf jq nss"

if [[ "$OS_FAMILY" == *"arch"* ]] || [[ "$OS" == "arch" ]]; then
    PACKAGES="$PACKAGES dnsmasq docker docker-compose"
elif [[ "$OS_FAMILY" == *"debian"* ]] || [[ "$OS" == "ubuntu" ]] || [[ "$OS" == "debian" ]]; then
    PACKAGES="$PACKAGES dnsmasq docker.io docker-compose-v2 libnss3-tools"
fi

info "Installing packages: $PACKAGES"
$PKG_MANAGER $PACKAGES

sudo systemctl enable --now docker
sudo usermod -aG docker $USER

success "Dependencies installed."
bash "$(dirname "$0")/03-environment.sh"
