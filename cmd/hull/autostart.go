package main

import (
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
)

func init() {
	autostart := &cobra.Command{
		Use:   "autostart",
		Short: "Choose what starts when Hull starts",
		Long: "Choose which projects and shared instances Hull brings up when the daemon\n" +
			"starts, so your usual setup is running after a reboot without starting each\n" +
			"thing by hand.\n" +
			"\n" +
			"`hull autostart add <name>` marks a project or a shared instance; `rm`\n" +
			"unmarks it; running it with no subcommand lists what is currently marked. A\n" +
			"project stores the flag in its own hull.yaml (autostart: true); a shared\n" +
			"instance is stored in config.\n" +
			"\n" +
			"On daemon start Hull brings these up WITHOUT re-running a project's setup\n" +
			"hooks (a boot is a resume, not a re-provision; run `hull up` for that). For\n" +
			"the items to actually be reachable, Hull itself must start at login too, so\n" +
			"pair this with `hull daemon enable`. A full reboot also needs Docker to\n" +
			"start at login (Docker Desktop's setting, or `systemctl enable docker`).",
		Example: "  hull autostart\n" +
			"  hull autostart add my-blog\n" +
			"  hull autostart add mysql-8.4\n" +
			"  hull autostart rm mysql-8.4",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return listAutostart() },
	}

	autostart.AddCommand(&cobra.Command{
		Use:     "add <name>",
		Short:   "Mark a project or shared instance to start with Hull",
		Args:    cobra.ExactArgs(1),
		Example: "  hull autostart add my-blog\n  hull autostart add mysql-8.4",
		RunE:    func(cmd *cobra.Command, args []string) error { return setAutostart(args[0], true) },
	})
	autostart.AddCommand(&cobra.Command{
		Use:     "rm <name>",
		Short:   "Stop a project or shared instance from starting with Hull",
		Args:    cobra.ExactArgs(1),
		Example: "  hull autostart rm mysql-8.4",
		RunE:    func(cmd *cobra.Command, args []string) error { return setAutostart(args[0], false) },
	})

	rootCmd.AddCommand(autostart)
}

// setAutostart marks (on) or unmarks (off) a project or shared instance for
// start-with-Hull. It resolves a project first, then a shared instance.
func setAutostart(name string, on bool) error {
	a, err := loadApp()
	if err != nil {
		return err
	}

	if p, ferr := a.findProject(name); ferr == nil {
		if err := a.Engine.SetProjectFields(p, engine.PatchOptions{Autostart: &on}); err != nil {
			return err
		}
		reportAutostart(p.Name, "project", on)
		warnDaemonAutostart(on)
		return nil
	}

	mgr := services.NewManager(a.Config)
	canonical := mgr.Canonical(name)
	if _, statErr := os.Stat(mgr.Dir(canonical)); statErr == nil {
		if err := updateAutostartInstance(a, canonical, on); err != nil {
			return err
		}
		reportAutostart(canonical, "instance", on)
		warnDaemonAutostart(on)
		return nil
	}

	return fmt.Errorf("no project or shared instance %q (see `hull list` and `hull services`)", name)
}

// updateAutostartInstance adds or removes a shared instance from the
// services.autostart config list and persists it.
func updateAutostartInstance(a *app, instance string, on bool) error {
	list := append([]string{}, a.Config.Services.Autostart...)
	has := slices.Contains(list, instance)
	switch {
	case on && !has:
		list = append(list, instance)
	case !on && has:
		list = slices.DeleteFunc(list, func(s string) bool { return s == instance })
	default:
		return nil // already in the desired state
	}
	if len(list) == 0 {
		list = nil
	}
	a.Config.Services.Autostart = list
	return a.Config.Save()
}

// listAutostart prints the projects and shared instances marked for autostart.
func listAutostart() error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	projects := []string{}
	if scanned, err := state.Scan(a.Config.Roots, a.Config.Projects...); err == nil {
		for i := range scanned {
			p := &scanned[i]
			if p.Manifest != nil && p.Manifest.Autostarts() {
				projects = append(projects, p.Name)
			}
		}
	}
	instances := append([]string{}, a.Config.Services.Autostart...)
	sort.Strings(projects)
	sort.Strings(instances)
	if flagJSON {
		return printJSON(struct {
			Projects []string `json:"projects"`
			Services []string `json:"services"`
		}{projects, instances})
	}
	if len(projects) == 0 && len(instances) == 0 {
		fmt.Println("Nothing set to autostart. Mark something with: hull autostart add <name>")
		return nil
	}
	if len(projects) > 0 {
		fmt.Println("Projects:")
		for _, n := range projects {
			fmt.Printf("  %s\n", n)
		}
	}
	if len(instances) > 0 {
		fmt.Println("Shared instances:")
		for _, n := range instances {
			fmt.Printf("  %s\n", n)
		}
	}
	if !platform.DaemonAutostartEnabled() {
		fmt.Println("\nNot starting at login. Enable with: hull daemon enable")
	}
	return nil
}

func reportAutostart(name, kind string, on bool) {
	verb := "will start"
	if !on {
		verb = "will no longer start"
	}
	fmt.Printf("✔ %s (%s) %s with Hull.\n", name, kind, verb)
}

// warnDaemonAutostart nudges the user to enable daemon-at-login, without which
// autostarted items are not actually served.
func warnDaemonAutostart(on bool) {
	if on && !platform.DaemonAutostartEnabled() {
		fmt.Println("  Not starting at login yet. Enable with: hull daemon enable")
	}
}
