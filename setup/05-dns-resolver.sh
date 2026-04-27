#!/bin/bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/ui.sh"
source "$(dirname "${BASH_SOURCE[0]}")/../.env"

step "Configuring DNS Resolver"

if [[ "${OS_FAMILY:-}" == *"arch"* ]] || [[ "${OS:-}" == "arch" ]]; then
    echo -e "[main]\ndns=dnsmasq" | sudo tee /etc/NetworkManager/conf.d/dns.conf > /dev/null
    sudo mkdir -p /etc/NetworkManager/dnsmasq.d
    echo "address=/.${TLD:-test}/127.0.0.1" | sudo tee /etc/NetworkManager/dnsmasq.d/hull-tld.conf > /dev/null
    sudo systemctl restart NetworkManager || true
elif [[ "${OS_FAMILY:-}" == *"debian"* ]] || [[ "${OS:-}" == "ubuntu" ]] || [[ "${OS:-}" == "debian" ]]; then
    sudo mkdir -p /etc/NetworkManager/dnsmasq.d
    echo "address=/.${TLD:-test}/127.0.0.1" | sudo tee /etc/NetworkManager/dnsmasq.d/hull-tld.conf > /dev/null
    sudo systemctl restart NetworkManager 2>/dev/null || sudo systemctl restart systemd-resolved 2>/dev/null || true
fi

success "DNS resolver configured for *.${TLD:-test}."
step "Hull installation complete."