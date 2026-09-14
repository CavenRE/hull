package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
				if err := a.Engine.Reload(cmd.Context(), p, service); err != nil {
					return fmt.Errorf("reloading %s: %w", p.Name, err)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&service, "service", "app", "compose service to reload")
	rootCmd.AddCommand(cmd)
}
