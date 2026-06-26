#!/bin/sh
# Hull bootstrap , install or uninstall straight from GitHub.
#
#   Install (desktop app + CLI):
#     curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh
#   Install CLI only:
#     curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh -s -- --cli
#   Uninstall:
#     curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh -s -- --uninstall
#     ( add --purge to also remove ~/.hull )
#
# Install path: downloads the prebuilt installer from the latest Release when one
# exists for your platform; otherwise clones the repo and builds from source
# (needs Go, plus Rust + WebKitGTK for the desktop app).
#
# Options:
#   --cli | --no-gui   install the CLI + daemon only (no desktop app)
#   --silent           don't open the installer window; install headless
#   --service          run hulld as a systemd --user service (Linux)
#   --prefix DIR       install binaries here (default: ~/.local/bin)
#   --source           skip the prebuilt download; always build from source
#   --uninstall        remove Hull from this machine
#   --purge            with --uninstall: also remove ~/.hull (config, CA, data)
#   -y, --yes          assume "yes" to prompts (non-interactive)
set -eu

REPO="CavenRE/hull"
BRANCH="master"
RAW="https://raw.githubusercontent.com/$REPO/$BRANCH"
RELEASE="https://github.com/$REPO/releases/latest/download"
CLONE="https://github.com/$REPO.git"

if [ -t 1 ]; then
  B=$(printf '\033[1m'); G=$(printf '\033[32m'); Y=$(printf '\033[33m'); R=$(printf '\033[31m'); X=$(printf '\033[0m')
else B=; G=; Y=; R=; X=; fi
say()  { printf '%s\n' "$*"; }
ok()   { printf '%s✔%s %s\n' "$G" "$X" "$*"; }
warn() { printf '%s!%s %s\n' "$Y" "$X" "$*"; }
die()  { printf '%s✗ %s%s\n' "$R" "$*" "$X" >&2; exit 1; }
step() { printf '\n%s> %s%s\n' "$B" "$*" "$X"; }
have() { command -v "$1" >/dev/null 2>&1; }

# ── args ─────────────────────────────────────────────────────────────────────
ACTION=install   # install | uninstall
MODE=gui         # gui | cli
SILENT=0
FORCE_SOURCE=0
SERVICE=0
PURGE=0
YES=0
PREFIX="${HOME}/.local/bin"
while [ $# -gt 0 ]; do
  case "$1" in
    --cli|--no-gui) MODE=cli ;;
    --gui) MODE=gui ;;
    --silent|--headless) SILENT=1 ;;
    --source|--build) FORCE_SOURCE=1 ;;
    --service) SERVICE=1 ;;
    --uninstall|--remove) ACTION=uninstall ;;
    --purge|--purge-data) PURGE=1 ;;
    --prefix) PREFIX="${2:?--prefix needs a directory}"; shift ;;
    --prefix=*) PREFIX="${1#*=}" ;;
    -y|--yes) YES=1 ;;
    -h|--help) sed -n '2,30p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
  shift
done

OS="$(uname -s)"
ARCH="$(uname -m)"

# ── download helpers ─────────────────────────────────────────────────────────
dl() { # dl URL OUTFILE , fails (non-zero) on HTTP error or 404
  if have curl; then curl -fsSL "$1" -o "$2"
  elif have wget; then wget -qO "$2" "$1"
  else die "need curl or wget to download"; fi
}

webkit_present() { ldconfig -p 2>/dev/null | grep -q 'libwebkit2gtk-4\.1'; }

# ── uninstall ────────────────────────────────────────────────────────────────
if [ "$ACTION" = uninstall ]; then
  step "Uninstalling Hull"
  HULL=""
  if [ -x "$PREFIX/hull" ]; then HULL="$PREFIX/hull"
  elif have hull; then HULL="$(command -v hull)"; fi

  if [ -n "$HULL" ]; then
    # The installed binary knows how to reverse everything (binaries, launcher,
    # icons, systemd unit, PATH). On a package install it defers to pacman.
    if [ "$PURGE" = 1 ]; then "$HULL" uninstall --quiet --purge-data; else "$HULL" uninstall --quiet; fi
  else
    # No binary on disk , fetch the standalone uninstall script and run it.
    warn "no hull binary found; fetching uninstall.sh"
    tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
    dl "$RAW/uninstall.sh" "$tmp" || die "could not download uninstall.sh"
    if [ "$PURGE" = 1 ]; then sh "$tmp" --prefix "$PREFIX" --purge; else sh "$tmp" --prefix "$PREFIX"; fi
  fi
  ok "Done."
  exit 0
fi

# ── install: prebuilt (Linux) ────────────────────────────────────────────────
try_prebuilt() {
  [ "$OS" = Linux ] || return 1
  [ "$FORCE_SOURCE" = 0 ] || return 1
  if ! webkit_present; then
    warn "WebKitGTK 4.1 not found , the prebuilt installer needs it; building from source instead"
    return 1
  fi
  tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
  # Try an arch-suffixed asset first, then a plain name.
  got=0
  for name in "hull-installer-linux-$ARCH" "hull-installer"; do
    if dl "$RELEASE/$name" "$tmp" 2>/dev/null; then got=1; break; fi
  done
  [ "$got" = 1 ] || { rm -f "$tmp"; return 1; }
  chmod +x "$tmp"

  step "Running the prebuilt Hull installer"
  set --
  [ "$PREFIX" != "$HOME/.local/bin" ] && set -- "$@" --dir "$PREFIX"
  [ "$SERVICE" = 1 ] && set -- "$@" --service
  if [ "$MODE" = cli ]; then
    "$tmp" --silent --no-gui "$@"
  elif [ "$SILENT" = 1 ]; then
    "$tmp" --silent "$@"
  else
    # Opens the WebKitGTK window; the binary installs headless on its own if no
    # display is available.
    "$tmp" "$@"
  fi
  rm -f "$tmp"
  return 0
}

# ── install: from source ─────────────────────────────────────────────────────
from_source() {
  have git || die "git is required to build from source (install git, or publish a Release)"
  step "Building Hull from source"
  src="$(mktemp -d)"; trap 'rm -rf "$src"' EXIT
  git clone --depth 1 "$CLONE" "$src" >/dev/null 2>&1 || die "git clone failed"

  set -- --prefix "$PREFIX"
  [ "$MODE" = cli ] && set -- "$@" --no-gui || set -- "$@" --gui
  [ "$SERVICE" = 1 ] && set -- "$@" --service
  [ "$YES" = 1 ] && set -- "$@" --yes
  ( cd "$src" && ./install.sh "$@" )
}

if ! try_prebuilt; then
  from_source
fi
