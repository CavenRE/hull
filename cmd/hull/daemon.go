package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/platform"
)

func init() {
	daemon := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the Hull daemon",
		Long: "Manage the Hull daemon: the background process that owns the router, DNS,\n" +
			"and shared-services manager. With no subcommand it reports daemon status.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runDaemonStatus(cmd) },
	}

	var background bool
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon in the foreground",
		Args:  cobra.NoArgs,
		Long: "Run the Hull daemon in the foreground. This is the same code that a\n" +
			"systemd unit or a detached launch runs; it is offered here so you can\n" +
			"start it directly, for development or a docker-run deployment.\n\n" +
			"On start it acquires a single-daemon lock (fails if one is already\n" +
			"running), binds to 127.0.0.1 on a random TCP port, generates a fresh\n" +
			"auth token, and writes the discovery file at ~/.hull/daemon.json (port,\n" +
			"token, PID) so the hull CLI can find and route to it. It then starts\n" +
			"networking (the router, plus DNS when enabled in config) and the /v1\n" +
			"HTTP server, and blocks until Ctrl-C or a client shutdown request.\n\n" +
			"Once it is up, every running project is served through the router and\n" +
			"mutating hull commands route through the daemon instead of running in\n" +
			"process. Run hull setup first if the router and DNS are not enabled yet.",
		Example: "  hull daemon run\n" +
			"  hull daemon run --home /custom/hull/home",
		RunE: func(cmd *cobra.Command, args []string) error {
			if background {
				// Launched by autostart (a Scheduled Task on Windows): drop the
				// console window the OS allocated for this console binary.
				hideConsole()
			}
			a, err := loadApp()
			if err != nil {
				return err
			}
			return runDaemon(a.Config)
		},
	}
	runCmd.Flags().BoolVar(&background, "background", false, "hide the console window (used by autostart)")
	_ = runCmd.Flags().MarkHidden("background")
	daemon.AddCommand(runCmd)

	daemon.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether a daemon is running",
		Long: "Report whether a Hull daemon is running, and if so its version, TLD,\n" +
			"and configured project roots.\n\n" +
			"It reads ~/.hull/daemon.json and makes a short (1.5 second) GET to the\n" +
			"daemon's /v1/status endpoint. If the discovery file is missing or the\n" +
			"daemon does not answer, it reports that none is running and exits 0 (not\n" +
			"an error): the CLI still works in process, just without daemon routing.\n\n" +
			"Use this to confirm the background daemon is up before relying on live\n" +
			"routing, or to check which version and roots it was started with.",
		Example: "  hull daemon status",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return runDaemonStatus(cmd) },
	})

	daemon.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop a running daemon",
		Long: "Stop the running Hull daemon, leaving projects and shared services as\n" +
			"they are.\n\n" +
			"It locates the daemon the same way status does (via ~/.hull/daemon.json)\n" +
			"and sends POST /v1/shutdown. The daemon finishes its in-flight HTTP\n" +
			"requests (up to a 5 second grace period) and exits, removing the\n" +
			"discovery file. If no daemon is running it prints a note and exits 0.\n\n" +
			"This stops only the daemon process: it does not stop your projects or\n" +
			"free the router and DNS ports. To bring everything down (projects,\n" +
			"shared services, and the daemon) use hull stop instead.",
		Example: "  hull daemon stop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			client, ok := api.Connect(a.Config.HullHome)
			if !ok {
				fmt.Println("No daemon running.")
				return nil
			}
			if err := client.Shutdown(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("Daemon stopped.")
			return nil
		},
	})

	// Deprecated spellings; the real home is `hull autostart enable|disable`.
	daemon.AddCommand(&cobra.Command{
		Use:    "enable",
		Short:  "Deprecated: use `hull autostart enable`",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(cmd *cobra.Command, args []string) error { return enableHullAtLogin(cmd) },
	})
	daemon.AddCommand(&cobra.Command{
		Use:    "disable",
		Short:  "Deprecated: use `hull autostart disable`",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(cmd *cobra.Command, args []string) error { return disableHullAtLogin() },
	})

	rootCmd.AddCommand(daemon)
}

// enableHullAtLogin registers the daemon to start at login and makes sure it is
// running now. Shared by `hull autostart enable`, the deprecated
// `hull daemon enable`, and `hull setup`.
func enableHullAtLogin(cmd *cobra.Command) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	startedNow, err := platform.EnableDaemonAutostart(exe)
	if err != nil {
		return err
	}
	fmt.Println("✔ Hull will start automatically at login.")
	// Windows' Run entry does not fire until the next login, so bring the daemon
	// up now; Linux/macOS already started it via their service manager.
	if !startedNow {
		if _, ok := api.Connect(a.Config.HullHome); !ok {
			fmt.Println("Starting Hull now...")
			if err := startDaemonDetached(cmd.Context(), a.Config.HullHome); err != nil {
				return err
			}
			fmt.Println("✔ Hull is running.")
		}
	}
	return nil
}

// disableHullAtLogin unregisters the daemon from launch-at-login. It does not
// stop a running daemon.
func disableHullAtLogin() error {
	if err := platform.DisableDaemonAutostart(); err != nil {
		return err
	}
	fmt.Println("✔ Hull will no longer start at login.")
	return nil
}

// runDaemonStatus reports whether a daemon is running plus the autostart state.
// Shared by `hull daemon status` and a bare `hull daemon`.
func runDaemonStatus(cmd *cobra.Command) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	client, ok := api.Connect(a.Config.HullHome)
	if !ok {
		fmt.Println("No daemon running (CLI operates in-process).")
		fmt.Printf("Autostart at login: %s\n", onOff(platform.DaemonAutostartEnabled()))
		return nil
	}
	st, err := client.Status(cmd.Context())
	if err != nil {
		return err
	}
	fmt.Printf("Daemon running: %s\n  TLD: %s\n  Roots: %v\n", st.Version, st.TLD, st.Roots)
	fmt.Printf("  Autostart at login: %s\n", onOff(platform.DaemonAutostartEnabled()))
	return nil
}

// onOff renders a boolean as an enabled/disabled label for status output.
func onOff(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
