#!/bin/sh
# Hull: make the paths a web app must write actually writable, on every boot.
#
# Why this exists: bind-mounted project files carry real Unix ownership (Docker
# Desktop's 9p mount uses the "metadata" option, and native Linux obviously
# does), while PHP serves as a non-root user. Anything created by root inside
# the container lands mode 0755 owned by root, and the web user then cannot
# write into it. That is what breaks WordPress media uploads ("the uploaded file
# could not be moved to wp-content/uploads/...") and Laravel's storage/.
#
# It resolves the run user the way the images themselves do, so one rule is
# correct on every platform: on Docker Desktop that is www-data; on native Linux
# Hull has already remapped that identity to the host user (serversideup) or set
# APACHE_RUN_USER to the host uid (wordpress), so the developer keeps ownership
# of their own files and can still edit them.
#
# Cheap by design: one stat per declared path, and it only walks a tree when
# that tree is genuinely owned by the wrong user. It replaces a recursive chmod
# of the whole webroot that cost about 7 seconds per boot over 9p.
#
# Runs either sourced (serversideup /etc/entrypoint.d) or executed (the
# wordpress entrypoint wrapper), so it keeps everything in a function and never
# calls exit. HULL_WRITABLE_PATHS is a space-separated list set by the renderer.
set +e

hull_fix_perms() {
	dir="${HULL_APP_DIR:-/var/www/html}"
	[ -d "$dir" ] || return 0
	[ -n "${HULL_WRITABLE_PATHS:-}" ] || return 0

	# Same resolution the wordpress image uses, including the "#1000" numeric
	# form Apache accepts. Falls back to www-data, which is correct for the PHP
	# images Hull ships.
	user="${APACHE_RUN_USER:-www-data}"
	group="${APACHE_RUN_GROUP:-www-data}"
	user="${user#\#}"
	group="${group#\#}"

	want="$(id -u "$user" 2>/dev/null)" || want=""
	[ -n "$want" ] || want="$user"

	cd "$dir" 2>/dev/null || return 0
	for p in $HULL_WRITABLE_PATHS; do
		if [ ! -e "$p" ]; then
			mkdir -p "$p" 2>/dev/null || continue
		fi
		# Only walk the tree when the owner is actually wrong. On a healthy boot
		# this is a single stat per path and nothing else.
		have="$(stat -c '%u' "$p" 2>/dev/null)"
		if [ "$have" != "$want" ]; then
			echo "hull: fixing ownership of $p (was uid ${have:-unknown}, want $user)"
			chown -R "$user:$group" "$p" 2>/dev/null || chown -R "$want" "$p" 2>/dev/null
		fi
		# Cheap and non-recursive: guarantees the owner can traverse and write
		# even if the mode was restrictive.
		chmod u+rwX "$p" 2>/dev/null
	done
	return 0
}

hull_fix_perms
