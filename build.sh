#!/usr/bin/env bash
# Build Hull's Linux graphical installer — one self-contained binary that
# embeds the GUI, daemon, CLI and icons, installs them, and wires uninstall to
# `hull uninstall`. Counterpart to build.ps1 on Windows.
#
#   ./build.sh
#
# Produces bin/hull-installer. Run it (no args) for the WebKitGTK install
# window, or `bin/hull-installer --silent` to install headless.
#
# Requires: go, cargo/rust, and WebKitGTK + GTK3 dev packages
#   (Arch: webkit2gtk-4.1 libayatana-appindicator gtk3)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; X=$'\033[0m'
step() { printf '\n%s> %s%s\n' "$B" "$*" "$X"; }
ok()   { printf '%s✔%s %s\n' "$G" "$X" "$*"; }
warn() { printf '%s!%s %s\n' "$Y" "$X" "$*"; }

VER="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
LDFLAGS="-s -w -X github.com/CavenRE/hull/internal/version.Version=${VER} -X github.com/CavenRE/hull/internal/version.Commit=${COMMIT}"

SETUP_DIR="$ROOT/cmd/hull-setup"
STAGE="$(mktemp -d)"
PCSHIM="$(mktemp -d)"
cleanup() { rm -rf "$STAGE" "$PCSHIM" "$SETUP_DIR/payload.tgz"; }
trap cleanup EXIT

# ── build CLI + daemon ───────────────────────────────────────────────────────
step "Building daemon + CLI"
go build -ldflags "$LDFLAGS" -o "$STAGE/hull"  ./cmd/hull
go build -ldflags "$LDFLAGS" -o "$STAGE/hulld" ./cmd/hulld
ok "hull + hulld ($VER)"

# ── build GUI ────────────────────────────────────────────────────────────────
step "Building GUI (cargo build --release)"
pkill -x hull-gui 2>/dev/null || true
cargo build --release --manifest-path gui/src-tauri/Cargo.toml
install -m 0755 gui/src-tauri/target/release/hull-gui "$STAGE/hull-gui"
ok "hull-gui"

# ── stage icons ──────────────────────────────────────────────────────────────
mkdir -p "$STAGE/icons"
install -m 0644 gui/src-tauri/icons/32x32.png      "$STAGE/icons/32x32.png"
install -m 0644 gui/src-tauri/icons/128x128.png    "$STAGE/icons/128x128.png"
install -m 0644 gui/src-tauri/icons/128x128@2x.png "$STAGE/icons/256x256.png"
install -m 0644 gui/src-tauri/icons/icon.png       "$STAGE/icons/512x512.png"

# ── pack payload ─────────────────────────────────────────────────────────────
step "Packing payload"
tar -czf "$SETUP_DIR/payload.tgz" -C "$STAGE" hull hulld hull-gui icons
ok "payload.tgz ($(du -h "$SETUP_DIR/payload.tgz" | cut -f1))"

# ── webkit2gtk-4.0 -> 4.1 pkg-config shim ────────────────────────────────────
# webview_go pins webkit2gtk-4.0, but modern distros (Arch) only ship 4.1. The
# APIs webview uses are source-compatible, so a shim .pc that Requires 4.1
# satisfies the lookup and links against the real 4.1 libraries.
PC_ENV=()
if ! pkg-config --exists webkit2gtk-4.0 2>/dev/null; then
  if pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
    cat > "$PCSHIM/webkit2gtk-4.0.pc" <<EOF
Name: webkit2gtk-4.0
Description: Compatibility shim mapping webkit2gtk-4.0 -> webkit2gtk-4.1
Version: $(pkg-config --modversion webkit2gtk-4.1)
Requires: webkit2gtk-4.1
EOF
    PC_ENV=(PKG_CONFIG_PATH="$PCSHIM:${PKG_CONFIG_PATH:-}")
    warn "using webkit2gtk-4.0 -> 4.1 pkg-config shim"
  else
    warn "WebKitGTK not found — install webkit2gtk-4.1 (Arch) or libwebkit2gtk-4.1-dev (Debian)"
  fi
fi

# ── build the installer (embeds payload.tgz, CGO + WebKitGTK) ─────────────────
step "Building installer (embeds payload)"
mkdir -p "$ROOT/bin"
env "${PC_ENV[@]}" CGO_ENABLED=1 \
  go build -tags installer -ldflags "$LDFLAGS" -o "$ROOT/bin/hull-installer" ./cmd/hull-setup

OUT="$ROOT/bin/hull-installer"
printf '\n'
ok "Installer ready: $OUT ($(du -h "$OUT" | cut -f1))"
echo "Run it for the install window, or: bin/hull-installer --silent  (headless)."
echo "Uninstall later with: hull uninstall"
