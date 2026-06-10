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
