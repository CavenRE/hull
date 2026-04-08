#!/bin/bash

BOLD="\033[1m"
GREEN="\033[1;32m"
BLUE="\033[1;34m"
YELLOW="\033[1;33m"
RED="\033[1;31m"
RESET="\033[0m"

info() {
    echo -e "${BLUE}i${RESET} ${BOLD}$1${RESET}"
}

success() {
    echo -e "${GREEN}v${RESET} ${BOLD}$1${RESET}"
}

warning() {
    echo -e "${YELLOW}!${RESET} $1"
}

fatal() {
    echo -e "${RED}x${RESET} $1"
    exit 1
}

step() {
    echo ""
    echo -e "${BLUE}>${RESET} ${BOLD}$1${RESET}"
    echo "----------------------------------------"
}
