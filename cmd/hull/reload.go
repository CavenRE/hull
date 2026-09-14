package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// reloadScript resets the PHP opcode cache in place, without recreating the
// container. It detects the server rather than the image, so it works on both
// families Hull ships: the upstream wordpress image (Apache with mod_php, where
// a graceful restart recycles workers and clears the cache) and serversideup
// (PHP-FPM, where the master reloads on SIGUSR2). pgrep -o picks the oldest
// php-fpm process, which is the master; under s6 that is not PID 1.
const reloadScript = `if command -v apachectl >/dev/null 2>&1; then ` +
	// apachectl always warns that it cannot determine the server's FQDN; that is
	// normal in a container and would read as an error here, so drop its stderr.
	// A genuine failure still shows up as a non-zero exit, which the caller reports.
	`apachectl -k graceful 2>/dev/null && echo "reloaded apache (opcode cache cleared)"; ` +
	`elif pid=$(pgrep -o php-fpm 2>/dev/null); then ` +
	`kill -USR2 "$pid" && echo "reloaded php-fpm (opcode cache cleared)"; ` +
	`else echo "no reloadable PHP server found in this container" >&2; exit 1; fi`

func init() {
	var service string
	cmd := &cobra.Command{
		Use:   "reload [name]",
		Short: "Clear a project's PHP opcode cache without restarting it",
		Long: "Reload the PHP server inside a project's app container so it picks up\n" +
			"changed PHP files, without recreating the container.\n" +
			"\n" +
			"This exists because the fastest OPcache setting for a project on a slow\n" +
			"filesystem is to stop revalidating files on every request. That removes a\n" +
			"large per-request cost, but means PHP keeps serving the compiled copy\n" +
			"until something tells it to let go. A reload is that signal, and it takes\n" +
			"about a second instead of the many seconds a full container restart costs.\n" +
			"\n" +
			"It detects the server rather than the image: a graceful restart on Apache\n" +
			"with mod_php, or SIGUSR2 to the PHP-FPM master. With no name it targets the\n" +
			"project in the current directory. The container must be running.",
		Example: "  hull reload\n" +
			"  hull reload myapp",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			targets, err := resolveTargets(cmd, a, args, false, "Reload", nil)
			if err != nil {
				return err
			}
			for _, p := range targets {
				if err := a.Engine.ExecIn(cmd.Context(), p, service, "sh", "-c", reloadScript); err != nil {
					return fmt.Errorf("reloading %s: %w", p.Name, err)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&service, "service", "app", "compose service to reload")
	rootCmd.AddCommand(cmd)
}
