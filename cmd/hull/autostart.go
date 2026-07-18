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
		Short: "Start Hull at login, and choose what comes up with it",
		Long: "Control what starts automatically: Hull itself at login, and which\n" +
			"projects and shared instances come up with it.\n" +
			"\n" +
			"`hull autostart enable` registers Hull to start when you log in (and starts\n" +
			"it now); `hull autostart disable` turns that off. Each platform uses its\n" +
			"native, no-elevation mechanism: a systemd --user unit on Linux (with\n" +
			"lingering, so it survives logout), a LaunchAgent on macOS, and a per-user\n" +
			"Run entry on Windows that launches the daemon with its console hidden.\n" +
			"\n" +
			"`hull autostart add <name>` then marks a project or shared instance to come\n" +
			"up with Hull; `rm` unmarks it. A project stores the flag in its own\n" +
			"hull.yaml (autostart: true); a shared instance is stored in config. Running\n" +
			"it with no subcommand shows the current state.\n" +
			"\n" +
			"On daemon start Hull brings marked items up WITHOUT re-running a project's\n" +
			"setup hooks (a boot is a resume, not a re-provision; run `hull up` for\n" +
			"that). A full reboot also needs Docker to start at login (Docker Desktop's\n" +
			"setting, or `systemctl enable docker`).",
		Example: "  hull autostart\n" +
			"  hull autostart enable\n" +
			"  hull autostart add my-blog\n" +
			"  hull autostart disable",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return listAutostart() },
	}

	autostart.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Start Hull automatically at login (and start it now)",
		Long: "Register Hull to start automatically when you log in, so your sites are\n" +
			"served without running `hull start` by hand. If Hull is not already\n" +
			"running, this starts it now too.\n\n" +
			"Linux uses a systemd --user unit with lingering, macOS a per-user\n" +
			"LaunchAgent, and Windows a per-user Run entry that launches the daemon\n" +
			"with its console hidden. None of them need administrator rights.\n\n" +
			"For containers to come back after a reboot, Docker itself must also start\n" +
			"at login (Docker Desktop's setting, or `systemctl enable docker`).",
		Example: "  hull autostart enable",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return enableHullAtLogin(cmd) },
	})

	autostart.AddCommand(&cobra.Command{
		Use:     "disable",
		Aliases: []string{"stop", "off"},
		Short:   "Stop Hull from starting at login",
		Long: "Unregister Hull from launch-at-login (the systemd --user unit, the\n" +
			"LaunchAgent, or the Run entry, depending on the platform).\n\n" +
			"This only removes the autostart entry; it does not stop a running daemon.\n" +
			"Use `hull daemon stop` to stop it now, or `hull stop` to bring everything\n" +
			"down.",
		Example: "  hull autostart disable",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return disableHullAtLogin() },
	})

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
	atLogin := platform.DaemonAutostartEnabled()
	if flagJSON {
		return printJSON(struct {
			HullAtLogin bool     `json:"hull_at_login"`
			Projects    []string `json:"projects"`
			Services    []string `json:"services"`
		}{atLogin, projects, instances})
	}

	// The headline is whether Hull itself comes back at login; the marked items
	// are meaningless without it.
	if atLogin {
		fmt.Println("Hull at login: enabled")
	} else {
		fmt.Println("Hull at login: disabled  (enable with: hull autostart enable)")
	}

	if len(projects) == 0 && len(instances) == 0 {
		fmt.Println("\nNothing marked to start with Hull. Mark something with: hull autostart add <name>")
		return nil
	}
	fmt.Println()
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
		fmt.Println("  Hull does not start at login yet. Enable with: hull autostart enable")
	}
}
