package main

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/doctor"
)

func init() {
	var fix bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the Hull environment",
		Long: "Diagnose the Hull environment and report what is healthy or broken.\n\n" +
			"It runs a series of checks over the container engine, your config.yaml,\n" +
			"networking, routing, and the daemon, printing each with a status icon\n" +
			"(ok, warn, or fail) and a short detail line. When a daemon is running it\n" +
			"also reports the daemon version.\n\n" +
			"On Windows it also measures how long a file operation actually takes\n" +
			"inside a container, since that one number explains most slow PHP pages,\n" +
			"and prints the antivirus exclusions worth making. Measuring writes a\n" +
			"temporary directory into your first affected root and removes it again;\n" +
			"nothing else here changes anything unless you pass --fix.\n\n" +
			"--fix applies the remedies Hull is confident about (creating a missing\n" +
			"project root, turning auto_reload back on) and reports what it did.\n" +
			"Anything that needs an elevated shell or moves your files is printed as a\n" +
			"command for you to run instead.\n\n" +
			"Run this after hull setup, or any time projects will not start or serve,\n" +
			"to find the first thing that is wrong. If any check is a blocking\n" +
			"failure the command exits non-zero, so it is safe to use in scripts as a\n" +
			"health gate. Warnings do not fail the run.",
		Example: "  hull doctor\n" +
			"  hull doctor --fix    (apply the safe remedies first)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			// Fixes run first so the report below describes the machine as it
			// now is, not as it was a moment ago.
			if fix {
				applied := doctor.ApplyFixes(a.Config)
				if len(applied) == 0 {
					fmt.Println("Nothing to fix automatically.")
				} else {
					for _, line := range applied {
						fmt.Printf(" • %s\n", line)
					}
				}
				fmt.Println()
			}

			deps := doctor.Deps{
				LookPath: exec.LookPath,
				Output:   dockerx.Output,
				// The person who typed this is waiting for an answer, so the
				// slow measurements are worth their few seconds here. The daemon
				// endpoint leaves them off so the GUI stays responsive.
				Measure: true,
			}
			if client, ok := a.client(); ok {
				if st, err := client.Status(cmd.Context()); err == nil {
					deps.DaemonVersion = st.Version
				}
			}

			checks := doctor.Run(cmd.Context(), a.Config, deps)
			icons := map[doctor.Status]string{doctor.OK: "✔", doctor.Warn: "!", doctor.Fail: "✖"}
			fixable := false
			for _, c := range checks {
				fmt.Printf(" %s %-28s %s\n", icons[c.Status], c.Name, c.Detail)
				// Commands are the point of their check, so they get their own
				// indented lines rather than being buried in the detail string.
				for _, cmdLine := range c.Commands {
					fmt.Printf("   %-28s %s\n", "", cmdLine)
				}
				if c.Fix != "" && !fix {
					fixable = true
					fmt.Printf("   %-28s hull doctor --fix: %s\n", "", c.Fix)
				}
			}
			if doctor.Fatal(checks) {
				return fmt.Errorf("doctor found blocking problems")
			}
			if fixable {
				fmt.Println("\nNo blocking problems found. Some warnings can be fixed with `hull doctor --fix`.")
			} else {
				fmt.Println("\nNo blocking problems found.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "apply the remedies Hull is confident about, then report")
	rootCmd.AddCommand(cmd)
}
