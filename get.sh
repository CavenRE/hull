#!/bin/sh
# Hull bootstrap , install or uninstall the CLI straight from GitHub.
#
#   Install:
#     curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh
#   Uninstall:
#     curl -fsSL https://raw.githubusercontent.com/CavenRE/hull/master/get.sh | sh -s -- --uninstall
#     ( add --purge to also remove ~/.hull )
#
# Install path: clones the repo and builds the CLI + daemon from source (needs Go).
#
# Options:
#   --service          run the daemon ("hull daemon run") as a systemd --user service (Linux)
#   --prefix DIR       install binaries here (default: ~/.local/bin)
#   --uninstall        remove Hull from this machine
#   --purge            with --uninstall: also remove ~/.hull (config, CA, data)
#   -y, --yes          assume "yes" to prompts (non-interactive)
set -eu

REPO="CavenRE/hull"
BRANCH="master"
RAW="https://raw.githubusercontent.com/$REPO/$BRANCH"
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
SERVICE=0
PURGE=0
YES=0
PREFIX="${HOME}/.local/bin"
while [ $# -gt 0 ]; do
  case "$1" in
    --service) SERVICE=1 ;;
    --uninstall|--remove) ACTION=uninstall ;;
    --purge|--purge-data) PURGE=1 ;;
    --prefix) PREFIX="${2:?--prefix needs a directory}"; shift ;;
    --prefix=*) PREFIX="${1#*=}" ;;
    -y|--yes) YES=1 ;;
    -h|--help) sed -n '2,18p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
  shift
done

# ── download helper ──────────────────────────────────────────────────────────
dl() { # dl URL OUTFILE , fails (non-zero) on HTTP error or 404
  if have curl; then curl -fsSL "$1" -o "$2"
  elif have wget; then wget -qO "$2" "$1"
  else die "need curl or wget to download"; fi
}

# ── uninstall ────────────────────────────────────────────────────────────────
if [ "$ACTION" = uninstall ]; then
  step "Uninstalling Hull"
  HULL=""
  if [ -x "$PREFIX/hull" ]; then HULL="$PREFIX/hull"
  elif have hull; then HULL="$(command -v hull)"; fi

  if [ -n "$HULL" ]; then
    # The installed binary knows how to reverse everything (binaries, systemd
    # unit, PATH). On a package install it defers to pacman.
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

# ── install: from source ─────────────────────────────────────────────────────
have git || die "git is required to build from source (install git)"
step "Building Hull from source"
src="$(mktemp -d)"; trap 'rm -rf "$src"' EXIT
git clone --depth 1 --branch "$BRANCH" "$CLONE" "$src" >/dev/null 2>&1 \
  || git clone --depth 1 "$CLONE" "$src" >/dev/null 2>&1 \
  || die "git clone failed"

set -- --prefix "$PREFIX"
[ "$SERVICE" = 1 ] && set -- "$@" --service
[ "$YES" = 1 ] && set -- "$@" --yes
( cd "$src" && ./install.sh "$@" )
