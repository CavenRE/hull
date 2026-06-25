#!/usr/bin/env bash
# Removes Hull's binaries, desktop integration, and (optionally) ~/.hull data.
# Leaves your projects and ~/.hull untouched unless --purge is given.
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

OS="$(uname -s)"
DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"

# Stop a running daemon and undo trust/DNS while the binary still exists.
if command -v "$PREFIX/hull" >/dev/null 2>&1; then
  "$PREFIX/hull" daemon stop 2>/dev/null || true
  "$PREFIX/hull" trust --uninstall 2>/dev/null || true
fi

# Linux desktop integration + systemd unit (paths match install.sh and
# internal/platform/desktop_linux.go).
if [ "$OS" = Linux ]; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl --user disable --now hulld.service 2>/dev/null || true
  fi
  rm -f "$CONFIG_HOME/systemd/user/hulld.service" 2>/dev/null || true
  command -v systemctl >/dev/null 2>&1 && systemctl --user daemon-reload 2>/dev/null || true

  rm -f "$DATA_HOME/applications/hull.desktop" 2>/dev/null || true
  rm -f "$CONFIG_HOME/autostart/hull.desktop"  2>/dev/null || true
  for size in 32x32 128x128 256x256 512x512; do
    rm -f "$DATA_HOME/icons/hicolor/$size/apps/hull.png" 2>/dev/null || true
  done
  command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$DATA_HOME/applications" 2>/dev/null || true
  command -v gtk-update-icon-cache  >/dev/null 2>&1 && gtk-update-icon-cache -f -t "$DATA_HOME/icons/hicolor" 2>/dev/null || true
  echo "removed desktop integration"
fi

# Strip the "# Added by Hull" PATH block from any shell rc it landed in.
for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do
  [ -f "$rc" ] || continue
  if grep -q '# Added by Hull' "$rc" 2>/dev/null; then
    # delete the marker line and the export PATH line right after it
    sed -i.hullbak '/# Added by Hull/{N;/export PATH=/d}' "$rc" 2>/dev/null || true
    rm -f "$rc.hullbak" 2>/dev/null || true
    echo "cleaned PATH in $rc"
  fi
done

for b in hull hulld hull-gui; do
  if [ -e "$PREFIX/$b" ] || [ -L "$PREFIX/$b" ]; then rm -f "$PREFIX/$b"; echo "removed $PREFIX/$b"; fi
done

if [ "$PURGE" = 1 ]; then
  read -r -p "Delete ~/.hull (config, CA, service data)? [y/N] " a
  case "$a" in y|Y|yes) rm -rf "$HOME/.hull"; echo "removed ~/.hull" ;; *) echo "kept ~/.hull" ;; esac
else
  echo "kept ~/.hull (use --purge to remove). Done."
fi
