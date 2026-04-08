#!/bin/bash
source "$(dirname "$0")/ui.sh"
source "$(dirname "$0")/../.env"

step "Configuring DNS Resolver"

if [[ "$OS_FAMILY" == *"arch"* ]] || [[ "$OS" == "arch" ]]; then
    echo -e "[main]\ndns=dnsmasq" | sudo tee /etc/NetworkManager/conf.d/dns.conf > /dev/null
    sudo mkdir -p /etc/NetworkManager/dnsmasq.d
    echo "address=/.$TLD/127.0.0.1" | sudo tee /etc/NetworkManager/dnsmasq.d/hull-tld.conf > /dev/null
    sudo systemctl restart NetworkManager
elif [[ "$OS_FAMILY" == *"debian"* ]] || [[ "$OS" == "ubuntu" ]] || [[ "$OS" == "debian" ]]; then
    sudo mkdir -p /etc/NetworkManager/dnsmasq.d
    echo "address=/.$TLD/127.0.0.1" | sudo tee /etc/NetworkManager/dnsmasq.d/hull-tld.conf > /dev/null
    sudo systemctl restart NetworkManager || sudo systemctl restart systemd-resolved
fi

success "DNS resolver configured for *.$TLD."
step "Hull installation complete."
