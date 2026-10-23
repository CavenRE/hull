package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/state"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "allow [name]",
		Short: "Approve a project so Hull may run it",
		Long: "Show what a project will run, and approve it.\n\n" +
			"Hull only starts, routes or runs the hooks of a project you approved,\n" +
			"and asks again when its hull.yaml (or a cluster's compose files)\n" +
			"changed since. Projects Hull creates for you are approved already; a\n" +
			"folder that appears by other means, such as a git clone into a root,\n" +
			"waits for this. With no name it targets the project in the current\n" +
			"directory. The prompt is skipped with --yes. Only this command takes\n" +
			"--yes as approval: on up, import and the others, --yes never approves.",
		Example: "  hull allow\n" +
			"  hull allow shop",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			if a.Engine.Trusted(p) {
				fmt.Printf("%s is already approved.\n", p.Name)
				return nil
			}
			if flagYes {
				err = a.Engine.Trust(p)
			} else {
				err = a.ensureTrusted(p)
			}
			if err != nil {
				return err
			}
			fmt.Printf("✔ %s approved.\n", p.Name)
			return nil
		},
	})
}

// ensureTrusted is the one place a terminal user approves a project before
// Hull runs it. The engine refuses an unapproved project on every path; this
// shows what it will run and records the answer.
func (a *app) ensureTrusted(p *state.Project) error {
	if a.Engine.Trusted(p) {
		return nil
	}
	// --yes skips prompts in scripts and the desktop app; approving code to run
	// must stay a decision someone made, so it takes `hull allow`.
	if flagYes {
		return fmt.Errorf("%s is not approved, and --yes does not approve a project; review it with `hull allow %s`", p.Name, p.Name)
	}
	fmt.Printf("Hull has not run %s (%s) in its current form. Running it:\n", p.Name, p.Dir)
	for _, line := range a.Engine.TrustSummary(p) {
		fmt.Println("  -", line)
	}
	ok, err := confirm("Approve " + p.Name + " and let Hull run it?")
	if err != nil {
		return fmt.Errorf("%s is not approved; review it, then run `hull allow %s` in a terminal", p.Name, p.Name)
	}
	if !ok {
		return fmt.Errorf("%s is not approved, so Hull did not run it", p.Name)
	}
	return a.Engine.Trust(p)
}
