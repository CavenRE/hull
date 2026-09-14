package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/state"
)

func init() {
	var (
		toWSL  bool
		distro string
		dest   string
		force  bool
	)

	cmd := &cobra.Command{
		Use:   "move <name> --to-wsl",
		Short: "Move a project into the WSL filesystem, where it runs at native speed",
		Long: "Move a project's files off your Windows drive and into a WSL2\n" +
			"distribution, which is the one change that actually removes Hull's\n" +
			"Windows performance problem instead of working around it.\n" +
			"\n" +
			"Why it is worth doing: Docker's engine runs inside a Linux VM, and a\n" +
			"project on a Windows drive reaches it over a mount where every file\n" +
			"operation costs milliseconds. Measured here on a 1,059 file WordPress\n" +
			"tree, a container reads it in 4,450 ms from a Windows drive and 10 ms\n" +
			"from a WSL distro, which is as fast as the container's own disk. That is\n" +
			"the difference between a page taking five seconds and taking one.\n" +
			"\n" +
			"What actually changes: only where the files are. Hull keeps running on\n" +
			"Windows and keeps serving the same https address. Docker identifies\n" +
			"containers and volumes by the project name rather than its folder, so\n" +
			"your databases and data volumes come through untouched.\n" +
			"\n" +
			"You keep editing the project normally. VS Code and the JetBrains IDEs\n" +
			"open the new path directly, and it appears in Explorer under Linux.\n" +
			"\n" +
			"Your original files are kept. The folder is renamed to\n" +
			"<name>.hull-moved so Hull stops seeing it as a second project of the\n" +
			"same name, and nothing inside it is deleted: check the project works at\n" +
			"its new home, then remove the old folder yourself.\n" +
			"\n" +
			"Requires Docker Desktop's WSL integration to be enabled for the distro\n" +
			"(Settings > Resources > WSL Integration). Hull checks and tells you if it\n" +
			"is not.",
		Example: "  hull move myapp --to-wsl\n" +
			"  hull move myapp --to-wsl --distro Ubuntu\n" +
			"  hull move myapp --to-wsl --dest /home/me/code/myapp",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !toWSL {
				return fmt.Errorf("say where to move it: --to-wsl is the only destination Hull supports today")
			}
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			if p.Manifest == nil {
				return fmt.Errorf("%s has no hull.yaml, so Hull does not know how to bring it back up after a move", p.Name)
			}

			if distro == "" {
				distros := platform.WSLDistros(cmd.Context())
				if len(distros) == 0 {
					return fmt.Errorf("no WSL distribution found. Install one first (for example: wsl --install -d Debian), then run this again")
				}
				distro = distros[0]
			}
			if dest == "" {
				dest, err = engine.WSLDestination(cmd.Context(), distro, p.Dir, p.Name)
				if err != nil {
					return err
				}
			}
			newDir := platform.UNCPath(distro, dest)

			fmt.Printf("Move %s\n  from %s\n  to   %s\n", p.Name, p.Dir, newDir)
			if !force {
				ok, err := confirm("The project will be stopped, copied, and started again. Continue?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("  Nothing moved.")
					return nil
				}
			}

			oldDir := p.Dir
			moved, retired, err := a.Engine.RelocateToWSL(cmd.Context(), p, engine.RelocateOptions{Distro: distro, Dest: dest},
				func(format string, v ...any) { fmt.Printf("  %s\n", fmt.Sprintf(format, v...)) })
			if err != nil {
				return err
			}

			// Hull's idea of where the project lives has to follow the files, or
			// the next command looks for it in the folder it just left.
			if err := a.reregisterProject(cmd.Context(), oldDir, moved); err != nil {
				return fmt.Errorf("your files are safely at %s, but Hull could not update its project list (%w). Nothing is lost: register the new location with `hull import %s`", moved, err, moved)
			}
			if err := a.regroupProject(cmd.Context(), oldDir, moved); err != nil {
				fmt.Printf("  ! the project kept its files but lost its group: %v\n", err)
			}

			fmt.Printf("  starting %s from its new home\n", p.Name)
			// Start through the daemon when there is one. It holds the project
			// lock and refreshes the route table on the way out, so the site is
			// actually served by the time this command claims it is; starting it
			// in-process would leave the router pointing at the old container
			// until its next sync.
			np := &state.Project{Name: p.Name, Dir: moved, Manifest: p.Manifest}
			if err := a.withDaemon(
				func(c *api.Client) error { return c.ProjectAction(cmd.Context(), p.Name, "start") },
				func() error { return a.Engine.Up(cmd.Context(), np) },
			); err != nil {
				return fmt.Errorf("the move itself succeeded and %s now lives at %s, but starting it there failed: %w. Your old files are still at %s if you want to go back", p.Name, moved, err, retired)
			}

			fmt.Printf("✔ %s now lives at %s\n", p.Name, moved)
			if p.Manifest.Served() && p.Manifest.Domain != "" {
				fmt.Printf("  served at https://%s.%s, same as before\n", p.Manifest.Domain, a.Config.TLD)
			}
			fmt.Printf("  your old files are still there, at %s; delete that folder once you are happy\n", retired)
			return nil
		},
	}

	cmd.Flags().BoolVar(&toWSL, "to-wsl", false, "move the project into a WSL2 distribution")
	cmd.Flags().StringVar(&distro, "distro", "", "which WSL distribution to move into (default: the first one installed)")
	cmd.Flags().StringVar(&dest, "dest", "", "destination directory as a Linux path inside the distro (default: ~/<parent folder>/<project>)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt")
	rootCmd.AddCommand(cmd)
}

// deregisterProject drops a directory from Hull's individually-registered
// project list. Called when a project is destroyed, so the list does not
// accumulate entries pointing at folders that no longer exist.
func (a *app) deregisterProject(ctx context.Context, dir string) error {
	return a.reregisterProject(ctx, dir, "")
}

// reregisterProject points Hull's project list at a project's new directory:
// it drops the old entry and adds the new one in a single save, so the project
// is never briefly registered twice or not at all. An empty newDir just drops
// the old entry.
func (a *app) reregisterProject(ctx context.Context, oldDir, newDir string) error {
	oldAbs, err := filepath.Abs(oldDir)
	if err != nil {
		return err
	}
	newAbs := ""
	if newDir != "" {
		if newAbs, err = filepath.Abs(newDir); err != nil {
			return err
		}
	}
	ci, err := a.configView(ctx)
	if err != nil {
		return err
	}
	kept := ci.Projects[:0:0] // a fresh slice: the view may be shared
	for _, p := range ci.Projects {
		if !sameRoot(p, oldAbs) {
			kept = append(kept, p)
		}
	}
	if newAbs == "" {
		ci.Projects = kept
		a.Config.Projects = kept
		return a.saveConfig(ctx, ci)
	}
	// A destination already covered by a parked root needs no entry of its own.
	registered := false
	for _, r := range ci.Roots {
		if state.Under(r, newAbs) {
			registered = true
			break
		}
	}
	for _, p := range kept {
		if sameRoot(p, newAbs) {
			registered = true
			break
		}
	}
	if !registered {
		kept = append(kept, newAbs)
	}
	ci.Projects = kept
	a.Config.Projects = kept
	return a.saveConfig(ctx, ci)
}

// regroupProject carries a project's virtual-group membership across a move.
// The store is keyed by directory, so without this the project silently drops
// out of whatever group the GUI had it in.
func (a *app) regroupProject(ctx context.Context, oldDir, newDir string) error {
	store, err := a.groupsView(ctx)
	if err != nil {
		return err
	}
	group := store.GroupOf(oldDir)
	if group == "" {
		return nil
	}
	store.SetMember(oldDir, "")
	store.SetMember(newDir, group)
	return a.saveGroups(ctx, store)
}
