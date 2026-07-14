#!/usr/bin/env bash
# Hull installer (Linux/macOS). Builds the CLI + daemon from source, checks
# dependencies, installs to ~/.local/bin, and hands off to `hull setup`.
# Windows users: see build.ps1.
#
# Usage:
#   ./install.sh [--prefix DIR] [--service] [--skip-setup] [--yes]
#
#   --prefix DIR  install binaries here (default: ~/.local/bin)
#   --service     run the Hull daemon as a systemd --user service (Linux)
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
SKIP_SETUP=0
ASSUME_YES=0
SERVICE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="${2:?--prefix needs a directory}"; shift ;;
    --service) SERVICE=1 ;;
    --skip-setup) SKIP_SETUP=1 ;;
    --yes|-y) ASSUME_YES=1 ;;
    -h|--help) sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
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

# Canonical XDG location for the systemd --user unit , matches
# internal/platform/desktop_linux.go so uninstall can clean up.
CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"

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
ExecStart=$PREFIX/hull daemon run
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload 2>/dev/null || true
  if systemctl --user enable --now hulld.service 2>/dev/null; then
    ok "hulld running as a systemd --user service"
    # Without linger a --user service is killed on logout and never starts at
    # boot , enable it so the daemon behaves like the background service we
    # just promised. Best-effort: may need polkit/root on some systems.
    if loginctl enable-linger "$USER" 2>/dev/null; then
      ok "linger enabled , hulld starts at boot and survives logout"
    else
      warn "enable boot-start/logout-survival with: sudo loginctl enable-linger $USER"
    fi
  else
    warn "installed hulld.service , enable it with: systemctl --user enable --now hulld.service"
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
  command -v go >/dev/null 2>&1 || die "Go not found , install it and re-run"
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

# ── build the CLI ───────────────────────────────────────────────────────────
step "Building Hull (CLI)"
VER="$(git -C "$REPO_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null || echo none)"
LDFLAGS="-s -w -X github.com/CavenRE/hull/internal/version.Version=${VER} -X github.com/CavenRE/hull/internal/version.Commit=${COMMIT}"
mkdir -p "$PREFIX"
go build -ldflags "$LDFLAGS" -o "$PREFIX/hull"  ./cmd/hull
ok "installed hull → $PREFIX (version $VER)"

# On Linux the daemon's embedded router and DNS bind 80/443/53 directly (no
# container), which needs CAP_NET_BIND_SERVICE. macOS lets non-root bind low
# ports; on Windows it's unrestricted. A fresh binary loses file caps, so
# re-apply here , before setup, so the daemon that setup brings up can bind.
if [ "$OS" = Linux ] && command -v setcap >/dev/null 2>&1; then
  if [ "$(getcap "$PREFIX/hull" 2>/dev/null)" = "" ]; then
    if confirm "  Grant hull permission to bind ports 80/443/53 (sudo setcap)?"; then
      if sudo setcap 'cap_net_bind_service=+ep' "$PREFIX/hull"; then
        ok "hull may now bind 80/443/53"
      else
        warn "setcap failed , run: sudo setcap 'cap_net_bind_service=+ep' $PREFIX/hull"
      fi
    else
      warn "without it, run the daemon as root or lower net.ipv4.ip_unprivileged_port_start"
    fi
  fi
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
      ok "updated $RC , open a new terminal or: export PATH=\"\$PATH:$PREFIX\""
    fi ;;
esac

# ── configure the machine (router, DNS, certificate) ─────────────────────────
# Run the same `hull setup` a user would , enable the native router + DNS,
# install the local CA into the trust store, and register *.tld with the OS
# resolver. It prompts for sudo only where it must (cert + DNS). Done BEFORE the
# daemon starts so the service comes up already configured , no manual restart.
DID_SETUP=0
if [ "$SKIP_SETUP" = 0 ] && [ "$DOCKER_OK" = 1 ]; then
  step "Configuring Hull (router, DNS, certificate)"
  setup_flags=""
  [ "$ASSUME_YES" = 1 ] && setup_flags="--yes"
  if "$PREFIX/hull" setup $setup_flags; then DID_SETUP=1; else warn "setup didn't finish , re-run: hull setup"; fi
fi

# ── run hulld as a systemd --user service (Linux) ────────────────────────────
# Installed AFTER setup so the daemon starts on the configured router/DNS.
# Opt-in: --service forces it; otherwise we only ask in an interactive run, so
# `--yes` automation never enables a background service behind the user's back.
want_service=0
if [ "$OS" = Linux ] && command -v systemctl >/dev/null 2>&1; then
  if [ "$SERVICE" = 1 ]; then
    want_service=1
  elif [ "$ASSUME_YES" = 0 ] && [ -t 0 ]; then
    confirm "  Run the Hull daemon in the background as a systemd --user service?" && want_service=1
  fi
  if [ "$want_service" = 1 ]; then step "Installing systemd --user service"; install_systemd_unit; fi
fi

# ── hand off ────────────────────────────────────────────────────────────────
if [ "$DID_SETUP" = 1 ]; then
  step "Verifying"
  "$PREFIX/hull" doctor || true
  say ""
  if [ "$want_service" = 1 ]; then
    ok "Hull is ready , the daemon is running. Scaffold a site: ${B}hull new myapp laravel${X}"
  else
    say "Start the daemon (${B}hull daemon run${X}, or re-run with ${B}--service${X}), then ${B}hull new myapp laravel${X}"
  fi
else
  step "Done"
  [ "$DOCKER_OK" = 0 ] && warn "install Docker, then run: hull setup"
  [ "$SKIP_SETUP" = 1 ] && say "Next: ${B}hull setup${X}, then ${B}hull daemon run${X}"
fi
ok "Hull $VER installed."
