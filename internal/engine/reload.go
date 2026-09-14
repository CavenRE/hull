package engine

import (
	"context"

	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// reloadScript resets the PHP opcode cache in place, without recreating the
// container. It detects the server rather than the image, so it covers both
// families Hull ships: the upstream wordpress image (Apache with mod_php, where
// a graceful restart recycles workers and drops the cache) and serversideup
// (PHP-FPM, whose master reloads on SIGUSR2). pgrep -o picks the oldest php-fpm
// process, which is the master; under s6 that is not PID 1.
//
// apachectl always warns that it cannot determine the server's FQDN, which is
// normal in a container and would read as an error, so its stderr is dropped. A
// genuine failure still surfaces as a non-zero exit.
const reloadScript = `if command -v apachectl >/dev/null 2>&1; then ` +
	`apachectl -k graceful 2>/dev/null && echo "reloaded apache (opcode cache cleared)"; ` +
	`elif pid=$(pgrep -o php-fpm 2>/dev/null); then ` +
	`kill -USR2 "$pid" && echo "reloaded php-fpm (opcode cache cleared)"; ` +
	`else echo "no reloadable PHP server found in this container" >&2; exit 1; fi`

// Reload clears a project's PHP opcode cache in place. It takes about a second,
// where recreating the container takes many, which is what makes it usable as
// the automatic answer to an edit.
func (e *Engine) Reload(ctx context.Context, p *state.Project, service string) error {
	if service == "" {
		service = "app"
	}
	return e.ExecIn(ctx, p, service, "sh", "-c", reloadScript)
}

// ReloadQuiet is Reload without a TTY, for the automatic path where a reload is
// routine and triggered by the daemon rather than typed by a person.
func (e *Engine) ReloadQuiet(ctx context.Context, p *state.Project) error {
	return e.composeFor(p).ExecNoTTY(ctx, "app", "sh", "-c", reloadScript)
}

// IsPHPProject reports whether a project runs on a PHP image, which is the only
// kind with an opcode cache worth reloading. Non-PHP runtimes either restart
// themselves on change or do not cache compiled code at all.
func IsPHPProject(p *state.Project) bool {
	if p.Manifest == nil {
		return false
	}
	def, ok := templates.Site(p.Manifest.Template)
	return ok && def.IsPHP()
}
