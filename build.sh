#!/usr/bin/env bash
# Build Hull's CLI + daemon into ./bin. Counterpart to build.ps1 on Windows.
#
#   ./build.sh
#
# Produces bin/hull and bin/hulld. To install into ~/.local/bin with dependency
# checks, PATH wiring, and an optional systemd --user unit, use ./install.sh.
#
# Requires: go.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

B=$'\033[1m'; G=$'\033[32m'; X=$'\033[0m'
step() { printf '\n%s> %s%s\n' "$B" "$*" "$X"; }
ok()   { printf '%s✔%s %s\n' "$G" "$X" "$*"; }

VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
LDFLAGS="-s -w -X github.com/CavenRE/hull/internal/version.Version=${VER} -X github.com/CavenRE/hull/internal/version.Commit=${COMMIT}"

step "Building daemon + CLI"
mkdir -p "$ROOT/bin"
go build -ldflags "$LDFLAGS" -o "$ROOT/bin/hull"  ./cmd/hull
go build -ldflags "$LDFLAGS" -o "$ROOT/bin/hulld" ./cmd/hulld
ok "bin/hull + bin/hulld ($VER)"
echo "Install into ~/.local/bin with: ./install.sh"
