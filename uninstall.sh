#!/usr/bin/env bash
# Removes Hull's binaries and (optionally) OS networking integration.
# Leaves your projects and ~/.hull data untouched unless --purge is given.
#
# Usage: ./uninstall.sh [--prefix DIR] [--purge]
#   --prefix DIR  where binaries were installed (default: ~/.local/bin)
#   --purge       also remove ~/.hull (config, local CA, service data) — destructive
set -euo pipefail
PREFIX="${HOME}/.local/bin"; PURGE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="${2:?}"; shift ;;
    --purge) PURGE=1 ;;
    -h|--help) sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac; shift
done

# Stop a running daemon and undo trust/DNS while the binary still exists.
if command -v "$PREFIX/hull" >/dev/null 2>&1; then
  "$PREFIX/hull" daemon stop 2>/dev/null || true
  "$PREFIX/hull" trust --uninstall 2>/dev/null || true
fi

for b in hull hulld hull-gui; do
  if [ -e "$PREFIX/$b" ] || [ -L "$PREFIX/$b" ]; then rm -f "$PREFIX/$b"; echo "removed $PREFIX/$b"; fi
done

if [ "$PURGE" = 1 ]; then
  read -r -p "Delete ~/.hull (config, CA, service data)? [y/N] " a
  case "$a" in y|Y|yes) rm -rf "$HOME/.hull"; echo "removed ~/.hull" ;; *) echo "kept ~/.hull" ;; esac
else
  echo "kept ~/.hull (use --purge to remove). Done."
fi
