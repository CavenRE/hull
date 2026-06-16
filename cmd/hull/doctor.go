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
		Long:  "Check the container engine, configuration, networking, and routing\npieces Hull depends on, with hints for anything broken.",
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
