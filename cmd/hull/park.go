package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

// editConfig is a quiet read-modify-write of the config (daemon-aware), for the
// park/unpark/forget commands that print their own confirmation instead of the
// generic one mutateConfig prints.
func editConfig(cmd *cobra.Command, mutate func(*configInfoT)) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	ci, err := a.configView(cmd.Context())
	if err != nil {
		return err
	}
	mutate(&ci)
	return a.saveConfig(cmd.Context(), ci)
}

// dirArg resolves an optional [dir] argument to an absolute path, defaulting to
// the current working directory.
func dirArg(args []string) (string, error) {
	target := "."
	if len(args) == 1 {
		target = args[0]
	}
	return filepath.Abs(target)
}

func init() {
	park := &cobra.Command{
		Use:   "park [dir]",
		Short: "Park a folder so Hull serves every project inside it",
		Long: "Register a folder as a Hull project root. Every project directory inside\n" +
			"it is scanned, listed, and served at <name>.<tld>. With no argument it\n" +
			"parks the folder you are standing in.\n" +
			"\n" +
			"Park a folder that CONTAINS projects. To manage a single project that\n" +
			"lives somewhere on its own, use `hull import` instead. This is the\n" +
			"friendly front end to `hull config roots add`.",
		Example: "  hull park\n" +
			"  hull park ~/Sites",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dirArg(args)
			if err != nil {
				return err
			}
			if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
				return fmt.Errorf("%s is not an existing directory", dir)
			}
			if fileExists(filepath.Join(dir, manifest.Filename)) {
				return fmt.Errorf("%s is itself a project (it has a hull.yaml); manage it with `hull import` instead of parking it", dir)
			}
			already := false
			if err := editConfig(cmd, func(ci *configInfoT) {
				if slices.ContainsFunc(ci.Roots, func(r string) bool { return sameRoot(r, dir) }) {
					already = true
					return
				}
				ci.Roots = append(ci.Roots, dir)
			}); err != nil {
				return err
			}
			if already {
				fmt.Printf("%s is already parked\n", dir)
			} else {
				fmt.Printf("✔ parked %s\n", dir)
			}
			return nil
		},
	}

	unpark := &cobra.Command{
		Use:   "unpark [dir]",
		Short: "Stop scanning a parked folder (files are left untouched)",
		Long: "Remove a folder from Hull's list of parked roots. Hull stops scanning it;\n" +
			"the folder and every project inside it are left exactly as they are on\n" +
			"disk. With no argument it unparks the folder you are standing in. This is\n" +
			"the friendly front end to `hull config roots rm`.",
		Example: "  hull unpark\n" +
			"  hull unpark ~/old-projects",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dirArg(args)
			if err != nil {
				return err
			}
			removed := false
			if err := editConfig(cmd, func(ci *configInfoT) {
				next := slices.DeleteFunc(ci.Roots, func(r string) bool { return sameRoot(r, dir) })
				removed = len(next) != len(ci.Roots)
				ci.Roots = next
			}); err != nil {
				return err
			}
			if removed {
				fmt.Printf("✔ unparked %s\n", dir)
			} else {
				fmt.Printf("%s was not a parked folder\n", dir)
			}
			return nil
		},
	}

	parked := &cobra.Command{
		Use:   "parked",
		Short: "List parked folders",
		Long: "List the folders Hull scans for projects, one absolute path per line.\n" +
			"Add --json for a JSON array. Individually-imported projects are not\n" +
			"listed here; see `hull list`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			ci, err := a.configView(cmd.Context())
			if err != nil {
				return err
			}
			if flagJSON {
				return printJSON(ci.Roots)
			}
			for _, r := range ci.Roots {
				fmt.Println(r)
			}
			return nil
		},
	}

	forget := &cobra.Command{
		Use:   "forget [name]",
		Short: "Stop managing a single imported project (files are kept)",
		Long: "Remove an individually-imported project from Hull's registry. Hull brings\n" +
			"it down so no containers or routes are left orphaned, then stops tracking\n" +
			"it, but never deletes your files (that is `hull rm`). With no argument it\n" +
			"forgets the project you are standing in.\n" +
			"\n" +
			"Only projects imported in place (outside a parked folder) can be\n" +
			"forgotten. For a project inside a parked folder, unpark the folder or use\n" +
			"`hull rm`.",
		Example: "  hull forget\n" +
			"  hull forget creative",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			var p *state.Project
			if len(args) == 1 {
				p, err = a.findProject(args[0])
				if err != nil {
					return err
				}
			} else {
				cur, ok := a.currentProject()
				if !ok {
					return fmt.Errorf("run `hull forget` inside a project, or pass its name")
				}
				p = cur
			}
			abs, _ := filepath.Abs(p.Dir)
			registered := slices.ContainsFunc(a.Config.Projects, func(d string) bool { return sameRoot(d, abs) })
			if !registered {
				return fmt.Errorf("%s lives under a parked folder; unpark the folder with `hull unpark`, or delete it with `hull rm %s`", p.Name, p.Name)
			}
			// Bring it down first so nothing is orphaned, then drop the
			// registration. Files stay on disk.
			if derr := a.Engine.Down(cmd.Context(), p); derr != nil {
				fmt.Printf("  note: could not fully stop %s (%v); forgetting anyway.\n", p.Name, derr)
			}
			if err := editConfig(cmd, func(ci *configInfoT) {
				ci.Projects = slices.DeleteFunc(ci.Projects, func(d string) bool { return sameRoot(d, abs) })
			}); err != nil {
				return err
			}
			fmt.Printf("✔ forgot %s (files kept at %s)\n", p.Name, p.Dir)
			return nil
		},
	}

	rootCmd.AddCommand(park, unpark, parked, forget)
}
