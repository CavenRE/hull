package main

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/doctor"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the Hull environment",
		Long: "Diagnose the Hull environment and report what is healthy or broken.\n\n" +
			"It runs a series of checks over the container engine, your config.yaml,\n" +
			"networking, routing, and the daemon, printing each with a status icon\n" +
			"(ok, warn, or fail) and a short detail line. When a daemon is running it\n" +
			"also reports the daemon version. The checks themselves run locally\n" +
			"against Docker and the filesystem; they do not mutate anything.\n\n" +
			"Run this after hull setup, or any time projects will not start or serve,\n" +
			"to find the first thing that is wrong. If any check is a blocking\n" +
			"failure the command exits non-zero, so it is safe to use in scripts as a\n" +
			"health gate. Warnings do not fail the run.",
		Example: "  hull doctor",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			deps := doctor.Deps{
				LookPath: exec.LookPath,
				Output:   dockerx.Output,
			}
			if client, ok := a.client(); ok {
				if st, err := client.Status(cmd.Context()); err == nil {
					deps.DaemonVersion = st.Version
				}
			}

			checks := doctor.Run(cmd.Context(), a.Config, deps)
			icons := map[doctor.Status]string{doctor.OK: "✔", doctor.Warn: "!", doctor.Fail: "✖"}
			for _, c := range checks {
				fmt.Printf(" %s %-28s %s\n", icons[c.Status], c.Name, c.Detail)
			}
			if doctor.Fatal(checks) {
				return fmt.Errorf("doctor found blocking problems")
			}
			fmt.Println("\nNo blocking problems found.")
			return nil
		},
	})
}
