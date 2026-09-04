#!/bin/sh
# Hull: seed the vendor/ named volume on first boot.
#
# Hull mounts vendor/ on a Docker named volume so Composer's thousands of files
# live on the fast container filesystem instead of the slow Windows/macOS bind
# mount. A fresh volume is empty and shadows the host vendor/, so this runs
# `composer install` into it before PHP-FPM ever serves a request. It is
# idempotent: a no-op once vendor is present, so it costs nothing after the
# first boot and self-heals after `hull reset` wipes the volume.
#
# serversideup sources every /etc/entrypoint.d/*.sh (in a subshell, under set -e)
# before it execs the web server, so the site never serves against an empty
# vendor/. This script is SOURCED, so it ends with `return`, not `exit`, and it
# runs `set +e` first: the parent's set -e would otherwise abort boot on the
# ordinary "vendor not yet installed" guard. Guards use `if` blocks (whose
# conditions are exempt from set -e anyway) to be doubly safe. A failed install
# is non-fatal: the site still starts, but 500s with a clear message until fixed,
# rather than crash-looping the container.
set +e
export COMPOSER_ALLOW_SUPERUSER=1
APP_DIR="${APP_BASE_DIR:-/var/www/html}"

# Nothing to seed for a project without Composer, or one already installed. The
# sentinel is a Hull-owned marker written ONLY after a fully successful install
# (below), not composer's own vendor/autoload.php: Composer writes autoload.php
# before running post-autoload-dump scripts (artisan package:discover), so a
# failure there would leave autoload.php present with a non-zero exit and be
# wrongly treated as "done". The marker avoids that: a partial/failed install
# leaves no marker, so the next boot retries.
if [ ! -f "$APP_DIR/composer.json" ]; then
	return 0 2>/dev/null || exit 0
fi
if [ -f "$APP_DIR/vendor/.hull-installed" ]; then
	return 0 2>/dev/null || exit 0
fi

echo "🚢 hull: installing Composer dependencies into the vendor volume (first boot, this can take a minute)..."
cd "$APP_DIR" 2>/dev/null || { return 0 2>/dev/null || exit 0; }
if composer install --no-interaction --prefer-dist --no-progress; then
	# Init runs as root (and, on native Linux, after Hull has already remapped
	# www-data to the host uid), so hand vendor to the PHP-FPM user; then the app
	# and `hull composer` can write to it at runtime.
	chown -R www-data:www-data "$APP_DIR/vendor" 2>/dev/null || true
	# Mark success last, so an interrupted or failed install is retried next boot
	# and Hull's post_create migrate can wait on this marker.
	: > "$APP_DIR/vendor/.hull-installed" 2>/dev/null || true
	echo "🚢 hull: Composer install complete."
else
	echo "🛑 hull: 'composer install' failed; the site will start but 500 until you fix composer.json and run 'hull composer install'." >&2
fi
return 0 2>/dev/null || exit 0
