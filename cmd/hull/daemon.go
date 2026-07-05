package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
)

func init() {
	daemon := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the Hull daemon",
	}

	daemon.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the daemon in the foreground (same as hulld)",
		Long: "Run the Hull daemon (hulld) in the foreground. This is the same process\n" +
			"the hulld binary runs; it is offered here so you can start it without a\n" +
			"separate executable, for development or a docker-run deployment.\n\n" +
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
			a, err := loadApp()
			if err != nil {
				return err
			}
			logf := func(format string, v ...any) { fmt.Printf(format+"\n", v...) }
			return api.Serve(cmd.Context(), a.Config, logf)
		},
	})

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
		Example: "  hull daemon status\n" +
			"  hull daemon status --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			client, ok := api.Connect(a.Config.HullHome)
			if !ok {
				fmt.Println("No daemon running (CLI operates in-process).")
				return nil
			}
			st, err := client.Status(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("Daemon running: %s\n  TLD: %s\n  Roots: %v\n", st.Version, st.TLD, st.Roots)
			return nil
		},
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

	rootCmd.AddCommand(daemon)
}
