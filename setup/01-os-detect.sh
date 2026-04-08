#!/bin/bash
source "$(dirname "$0")/ui.sh"

step "Detecting Operating System"

if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
    OS_FAMILY=$ID_LIKE
else
    fatal "/etc/os-release is missing."
fi

if [[ "$OS_FAMILY" == *"arch"* ]] || [[ "$OS" == "arch" ]]; then
    PKG_MANAGER="sudo pacman -S --needed --noconfirm"
    UPDATE_CMD="sudo pacman -Sy"
    success "Arch Linux base detected."
elif [[ "$OS_FAMILY" == *"debian"* ]] || [[ "$OS" == "ubuntu" ]] || [[ "$OS" == "debian" ]]; then
    PKG_MANAGER="sudo apt-get install -y"
    UPDATE_CMD="sudo apt-get update"
    success "Debian/Ubuntu base detected."
else
    fatal "Unsupported OS: $OS."
fi

export PKG_MANAGER
export UPDATE_CMD
export OS_FAMILY

bash "$(dirname "$0")/02-dependencies.sh"
