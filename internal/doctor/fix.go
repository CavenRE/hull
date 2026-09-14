package doctor

import (
	"fmt"
	"os"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/templates"
)

// ApplyFixes performs the remedies Hull is confident enough to apply without
// being asked twice, and returns one line per action taken.
//
// The bar for membership is deliberately high: the change has to be reversible,
// local to Hull, and something the user would obviously have done themselves.
// Anything that needs elevation, touches security settings, moves files, or
// could surprise someone stays out, and is surfaced as a command on the check
// instead. That is why there is no entry here for the antivirus exclusions or
// for moving a project into WSL.
//
// Fixes run before the checks, so the report the user reads afterwards reflects
// the machine as it now is rather than as it was.
func ApplyFixes(cfg *config.Config) []string {
	var done []string

	// A root Hull is configured to scan but that does not exist yet. Creating
	// it is what every user does by hand the first time they see the warning.
	for _, root := range cfg.Roots {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			continue
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			done = append(done, "could not create root "+root+": "+err.Error())
			continue
		}
		done = append(done, "created root "+root)
	}

	// auto_reload off means PHP re-checks every cached file on every request,
	// which on a slow mount roughly doubles a page load. Turning it back on is
	// one line of config and is undone the same way.
	//
	// Only where it costs something. On a machine with no project on a slow
	// filesystem the setting changes nothing, no check ever offers to change it,
	// and silently flipping a value the user deliberately set would be exactly
	// the kind of surprise --fix must not produce.
	if !cfg.AutoReloadEnabled() && anySlowRoot(cfg) {
		cfg.AutoReload = nil
		if err := cfg.Save(); err != nil {
			done = append(done, "could not turn auto_reload back on: "+err.Error())
		} else {
			done = append(done, "turned auto_reload back on (restart the daemon to pick it up: hull stop && hull start)")
		}
	}

	// Hull's own support files. Run already self-heals these silently; doing it
	// here too means --fix reports it rather than it happening invisibly.
	if err := templates.EnsureSystemFiles(cfg.HullHome); err != nil {
		done = append(done, fmt.Sprintf("could not write Hull's system files: %v", err))
	}

	return done
}

// anySlowRoot reports whether any configured root is on the filesystem where
// Hull's performance mitigations actually matter.
func anySlowRoot(cfg *config.Config) bool {
	for _, root := range cfg.Roots {
		if platform.OnWindowsFilesystem(root) {
			return true
		}
	}
	return false
}
