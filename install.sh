#!/usr/bin/env bash
# Hull installer (Linux/macOS). Builds the CLI (and optionally the GUI) from
# source, checks dependencies, installs to ~/.local/bin, and hands off to
# `hull setup`. Windows users: see install.ps1.
#
# Usage:
#   ./install.sh [--no-gui|--gui] [--prefix DIR] [--service] [--skip-setup] [--yes]
#
#   --no-gui      CLI only (default if the GUI toolchain is absent)
#   --gui         build the Tauri GUI too (requires Rust + webkit/appindicator)
#   --prefix DIR  install binaries here (default: ~/.local/bin)
#   --service     run hulld as a systemd --user service (Linux)
#   --skip-setup  don't run `hull setup`/`hull doctor` at the end
#   --yes         assume "yes" to install prompts (non-interactive)
set -euo pipefail

# ── pretty output ───────────────────────────────────────────────────────────
if [ -t 1 ]; then B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; D=$'\033[2m'; X=$'\033[0m'
else B=; G=; Y=; R=; D=; X=; fi
say()  { printf '%s\n' "$*"; }
ok()   { printf '%s✔%s %s\n' "$G" "$X" "$*"; }
warn() { printf '%s!%s %s\n' "$Y" "$X" "$*"; }
die()  { printf '%s✗ %s%s\n' "$R" "$*" "$X" >&2; exit 1; }
step() { printf '\n%s> %s%s\n' "$B" "$*" "$X"; }

# ── args ────────────────────────────────────────────────────────────────────
PREFIX="${HOME}/.local/bin"
WANT_GUI=auto
SKIP_SETUP=0
ASSUME_YES=0
SERVICE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --no-gui) WANT_GUI=no ;;
    --gui) WANT_GUI=yes ;;
    --prefix) PREFIX="${2:?--prefix needs a directory}"; shift ;;
    --service) SERVICE=1 ;;
    --skip-setup) SKIP_SETUP=1 ;;
    --yes|-y) ASSUME_YES=1 ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown flag: $1 (try --help)" ;;
  esac
  shift
done

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_DIR"

confirm() { # confirm "question" -> 0 yes / 1 no
  [ "$ASSUME_YES" = 1 ] && return 0
  [ -t 0 ] || return 1
  printf '%s [y/N] ' "$1"; read -r a; case "$a" in y|Y|yes) return 0 ;; *) return 1 ;; esac
}

# Canonical XDG locations — these MUST match internal/platform/desktop_linux.go
# so `hull uninstall` / uninstall.sh can clean up whatever installed Hull.
DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"

# install_desktop_files installs the menu launcher + hicolor icons for the GUI.
# Linux only (macOS doesn't use .desktop files); call only when hull-gui exists.
install_desktop_files() {
  local apps="$DATA_HOME/applications" icons="$DATA_HOME/icons/hicolor"
  local src="$REPO_DIR/gui/src-tauri/icons"
  mkdir -p "$apps"
  # size_dir:source-file — matches the sizes RemoveIcons() cleans up.
  for pair in "32x32:32x32.png" "128x128:128x128.png" "256x256:128x128@2x.png" "512x512:icon.png"; do
    local size="${pair%%:*}" file="${pair##*:}"
    [ -f "$src/$file" ] || continue
    mkdir -p "$icons/$size/apps"
    install -m 0644 "$src/$file" "$icons/$size/apps/hull.png"
  done
  cat > "$apps/hull.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Hull
GenericName=Local Web Environment
Comment=A local environment for your sites & apps
Exec=$PREFIX/hull-gui
Icon=hull
Terminal=false
Categories=Development;
StartupWMClass=Hull
EOF
  command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps" 2>/dev/null || true
  command -v gtk-update-icon-cache  >/dev/null 2>&1 && gtk-update-icon-cache -f -t "$icons" 2>/dev/null || true
  ok "installed menu launcher + icons"
}

# install_systemd_unit installs (and tries to enable) a systemd --user unit so
# hulld runs in the background. Matches WriteSystemdUserUnit() in Go.
install_systemd_unit() {
  local unit_dir="$CONFIG_HOME/systemd/user"
  mkdir -p "$unit_dir"
  cat > "$unit_dir/hulld.service" <<EOF
[Unit]
Description=Hull daemon (local router, DNS, services)
After=network-online.target docker.service
Wants=network-online.target

[Service]
ExecStart=$PREFIX/hulld daemon run
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload 2>/dev/null || true
  if systemctl --user enable --now hulld.service 2>/dev/null; then
    ok "hulld running as a systemd --user service"
  else
    warn "installed hulld.service — enable it with: systemctl --user enable --now hulld.service"
  fi
}

# ── platform + package manager detection ────────────────────────────────────
OS="$(uname -s)"; ARCH="$(uname -m)"
PM=""; PM_INSTALL=""
case "$OS" in
  Linux)
    if [ -r /etc/os-release ]; then . /etc/os-release; fi
    if command -v pacman >/dev/null 2>&1;  then PM=pacman;  PM_INSTALL="sudo pacman -S --needed --noconfirm"
    elif command -v apt-get >/dev/null 2>&1; then PM=apt;   PM_INSTALL="sudo apt-get install -y"
    elif command -v dnf >/dev/null 2>&1;   then PM=dnf;     PM_INSTALL="sudo dnf install -y"
    fi ;;
  Darwin)
    command -v brew >/dev/null 2>&1 && { PM=brew; PM_INSTALL="brew install"; } ;;
  *) die "unsupported OS: $OS (use install.ps1 on Windows)" ;;
esac
say "${D}platform: $OS/$ARCH  package manager: ${PM:-none}${X}"

# offer to install a package via the detected manager; return 1 if we can't / declined
maybe_install() { # maybe_install "human name" "pkg-for-pm" "https://link"
  local name="$1" pkg="$2" link="$3"
  if [ -n "$PM" ] && [ -n "$pkg" ]; then
    if confirm "  Install $name with $PM ($PM_INSTALL $pkg)?"; then
      # shellcheck disable=SC2086
      $PM_INSTALL $pkg && return 0 || warn "auto-install failed"
    fi
  fi
  warn "install $name manually: $link"
  return 1
}

# ── dependency checks ───────────────────────────────────────────────────────
step "Checking dependencies"

# Go (required to build from source)
if command -v go >/dev/null 2>&1; then
  ok "go $(go version | awk '{print $3}')"
else
  warn "Go is required to build Hull"
  case "$PM" in
    pacman) maybe_install "Go" "go" "https://go.dev/dl/" ;;
    apt)    maybe_install "Go" "golang-go" "https://go.dev/dl/" ;;
    dnf)    maybe_install "Go" "golang" "https://go.dev/dl/" ;;
    brew)   maybe_install "Go" "go" "https://go.dev/dl/" ;;
    *)      warn "install Go: https://go.dev/dl/" ;;
  esac
  command -v go >/dev/null 2>&1 || die "Go not found — install it and re-run"
fi

# Docker engine + compose plugin (required at runtime, not to build)
DOCKER_OK=1
if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then ok "docker + compose plugin"
  else warn "docker found but the compose plugin is missing"; DOCKER_OK=0
       maybe_install "docker compose plugin" \
         "$([ "$PM" = pacman ] && echo docker-compose || echo docker-compose-plugin)" \
         "https://docs.docker.com/compose/install/" || true
  fi
else
  warn "Docker is not installed (Hull needs it to run containers)"; DOCKER_OK=0
  case "$PM" in
    pacman) maybe_install "Docker" "docker docker-compose" "https://docs.docker.com/engine/install/" || true ;;
    apt)    warn "install Docker: https://docs.docker.com/engine/install/ubuntu/" ;;
    brew)   maybe_install "Docker Desktop" "--cask docker" "https://docs.docker.com/desktop/install/mac-install/" || true ;;
    *)      warn "install Docker: https://docs.docker.com/engine/install/" ;;
  esac
fi

# GUI toolchain (optional)
GUI_BUILDABLE=1
have_cargo() { command -v cargo >/dev/null 2>&1; }
gui_linux_libs_ok() {
  command -v pkg-config >/dev/null 2>&1 || return 1
  pkg-config --exists webkit2gtk-4.1 || return 1
  # appindicator: either ayatana or the classic lib satisfies Tauri's tray
  pkg-config --exists ayatana-appindicator3-0.1 || pkg-config --exists appindicator3-0.1 || return 1
}
if [ "$WANT_GUI" != no ]; then
  have_cargo || { GUI_BUILDABLE=0; [ "$WANT_GUI" = yes ] && warn "Rust/cargo missing — needed for the GUI (https://rustup.rs)"; }
  if [ "$OS" = Linux ] && ! gui_linux_libs_ok; then
    GUI_BUILDABLE=0
    if [ "$WANT_GUI" = yes ] || { [ "$WANT_GUI" = auto ] && have_cargo; }; then
      warn "GUI system libs missing (webkit2gtk-4.1 / appindicator)"
      case "$PM" in
        pacman) maybe_install "GUI libs" "webkit2gtk-4.1 libayatana-appindicator" \
                  "https://v2.tauri.app/start/prerequisites/" && gui_linux_libs_ok && GUI_BUILDABLE=1 || true ;;
        apt)    maybe_install "GUI libs" "libwebkit2gtk-4.1-dev libayatana-appindicator3-dev librsvg2-dev" \
                  "https://v2.tauri.app/start/prerequisites/" && gui_linux_libs_ok && GUI_BUILDABLE=1 || true ;;
        *)      warn "Tauri Linux prerequisites: https://v2.tauri.app/start/prerequisites/" ;;
      esac
    fi
  fi
fi
# resolve the final GUI decision
BUILD_GUI=0
case "$WANT_GUI" in
  yes)  [ "$GUI_BUILDABLE" = 1 ] && BUILD_GUI=1 || die "GUI requested but toolchain is incomplete (see notes above)" ;;
  auto) [ "$GUI_BUILDABLE" = 1 ] && have_cargo && BUILD_GUI=1 ;;
  no)   BUILD_GUI=0 ;;
esac
[ "$BUILD_GUI" = 1 ] && ok "GUI will be built" || say "${D}GUI build skipped (CLI-only). Re-run with --gui once the toolchain is ready.${X}"

# ── build the CLI ───────────────────────────────────────────────────────────
step "Building Hull (CLI)"
VER="$(git -C "$REPO_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null || echo none)"
LDFLAGS="-s -w -X github.com/CavenRE/hull/internal/version.Version=${VER} -X github.com/CavenRE/hull/internal/version.Commit=${COMMIT}"
mkdir -p "$PREFIX"
go build -ldflags "$LDFLAGS" -o "$PREFIX/hull"  ./cmd/hull
go build -ldflags "$LDFLAGS" -o "$PREFIX/hulld" ./cmd/hulld
ok "installed hull + hulld → $PREFIX (version $VER)"

# On Linux the daemon's embedded router binds 80/443 directly (no container),
# which needs CAP_NET_BIND_SERVICE. macOS lets non-root bind low ports; on
# Windows it's unrestricted. A fresh binary loses file caps, so re-apply here.
if [ "$OS" = Linux ] && command -v setcap >/dev/null 2>&1; then
  if [ "$(getcap "$PREFIX/hulld" 2>/dev/null)" = "" ]; then
    if confirm "  Grant hulld permission to bind ports 80/443 (sudo setcap)?"; then
      if sudo setcap 'cap_net_bind_service=+ep' "$PREFIX/hulld"; then
        ok "hulld may now bind 80/443"
      else
        warn "setcap failed — run: sudo setcap 'cap_net_bind_service=+ep' $PREFIX/hulld"
      fi
    else
      warn "without it, run the daemon as root or lower net.ipv4.ip_unprivileged_port_start"
    fi
  fi
fi

# ── build the GUI ───────────────────────────────────────────────────────────
if [ "$BUILD_GUI" = 1 ]; then
  step "Building Hull (GUI)"
  cargo build --release --manifest-path gui/src-tauri/Cargo.toml
  GUI_BIN="$REPO_DIR/gui/src-tauri/target/release/hull-gui"
  if [ -f "$GUI_BIN" ]; then
    install -m 0755 "$GUI_BIN" "$PREFIX/hull-gui"
    ok "installed hull-gui → $PREFIX"
    [ "$OS" = Linux ] && install_desktop_files
  else
    warn "GUI build finished but binary not found at $GUI_BIN"
  fi
fi

# ── optional: run hulld as a systemd --user service (Linux) ──────────────────
# Opt-in: --service forces it; otherwise we only ask in an interactive run, so
# `--yes` automation never enables a background service behind the user's back.
if [ "$OS" = Linux ] && command -v systemctl >/dev/null 2>&1; then
  want_service=0
  if [ "$SERVICE" = 1 ]; then
    want_service=1
  elif [ "$ASSUME_YES" = 0 ] && [ -t 0 ]; then
    confirm "  Run hulld in the background as a systemd --user service?" && want_service=1
  fi
  [ "$want_service" = 1 ] && { step "Installing systemd --user service"; install_systemd_unit; }
fi

# ── PATH ────────────────────────────────────────────────────────────────────
case ":$PATH:" in
  *":$PREFIX:"*) : ;;
  *)
    step "Adding $PREFIX to PATH"
    case "${SHELL:-}" in
      *zsh)  RC="$HOME/.zshrc" ;;
      *bash) RC="${HOME}/.bashrc"; [ -f "$RC" ] || RC="$HOME/.bash_profile" ;;
      *)     RC="$HOME/.profile" ;;
    esac
    if ! grep -qs 'Added by Hull' "$RC" 2>/dev/null; then
      printf '\n# Added by Hull\nexport PATH="$PATH:%s"\n' "$PREFIX" >> "$RC"
      ok "updated $RC — open a new terminal or: export PATH=\"\$PATH:$PREFIX\""
    fi ;;
esac

# ── hand off ────────────────────────────────────────────────────────────────
if [ "$SKIP_SETUP" = 0 ] && [ "$DOCKER_OK" = 1 ]; then
  step "Running diagnostics"
  "$PREFIX/hull" doctor || true
  say ""
  say "Next: ${B}hull setup${X}  (enables the native router + DNS; may prompt for sudo)"
  say "Then: ${B}hulld${X}        (start the daemon) and ${B}hull up${X}"
else
  step "Done"
  [ "$DOCKER_OK" = 0 ] && warn "install Docker, then run: hull doctor"
fi
ok "Hull $VER installed."
