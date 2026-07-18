package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// listGroups prints every group and membership, sorted for stable output.
// Shared by `hull group list` and a bare `hull group`.
func listGroups(cmd *cobra.Command) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	store, err := a.groupsView(cmd.Context())
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(store)
	}
	roots := make([]string, 0, len(store.Roots))
	for root := range store.Roots {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		fmt.Printf("%s\n", root)
		for _, g := range store.Roots[root].Groups {
			fmt.Printf("  %s\n", g)
		}
	}
	if len(store.Members) > 0 {
		dirs := make([]string, 0, len(store.Members))
		for dir := range store.Members {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		fmt.Println("members:")
		for _, dir := range dirs {
			fmt.Printf("  %s -> %s\n", dir, store.Members[dir])
		}
	}
	return nil
}

func init() {
	grp := &cobra.Command{
		Use:   "group",
		Short: "Organize projects into virtual groups",
		Long: "Organize projects into virtual groups: purely organizational labels shown\n" +
			"inside each project root (for example in the GUI's project list).\n\n" +
			"Groups live entirely on Hull's side. They are stored in groups.yaml in your\n" +
			"Hull home directory, keyed by project path, so nothing inside your project\n" +
			"folders changes and even unmanaged folders can be grouped. Group definitions\n" +
			"are scoped per root; project membership is tracked per project directory.\n\n" +
			"Use the subcommands to inspect (list) or change (add, rm, order, mv)\n" +
			"grouping. All of them route through a running daemon when one is up,\n" +
			"otherwise they read or write groups.yaml directly. With no subcommand it\n" +
			"lists the groups.",
		Example: "  hull group list\n" +
			"  hull group add ~/Sites backend\n" +
			"  hull group mv shop backend\n" +
			"  hull group rm ~/Sites backend",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return listGroups(cmd) },
	}

	grp.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List groups and members",
		Long: "List every group and project membership across all configured roots.\n\n" +
			"The default output is a small text tree: each root is printed, then the\n" +
			"groups defined under it, followed by a members section mapping each project\n" +
			"directory to the group it belongs to. When a daemon is running this reads\n" +
			"its group store, otherwise it loads groups.yaml directly. It is read-only.\n\n" +
			"Add --json to emit the full store as an object, with a roots map (each root\n" +
			"listing its groups) and a members map from directory to group name.",
		Example: "  hull group list\n" +
			"  hull group list --json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return listGroups(cmd) },
	})

	grp.AddCommand(&cobra.Command{
		Use:   "add <root> <name>",
		Short: "Create a group inside a project root",
		Long: "Create a new, empty group inside a project root. The first argument is the\n" +
			"root (its configured full path or just its base name, matched\n" +
			"case-insensitively) and the second is the group label to create.\n\n" +
			"This only defines the group; it does not move any project into it. Use\n" +
			"hull group mv to assign projects. Adding a group that already exists under\n" +
			"the root is a no-op, so re-running is safe. A running daemon applies the\n" +
			"change live, otherwise it edits groups.yaml directly.",
		Example: "  hull group add ~/Sites backend\n" +
			"  hull group add Sites frontend",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			root, err := resolveRoot(a, args[0])
			if err != nil {
				return err
			}
			store, err := a.groupsView(cmd.Context())
			if err != nil {
				return err
			}
			store.AddGroup(root, args[1])
			if err := a.saveGroups(cmd.Context(), store); err != nil {
				return err
			}
			fmt.Printf("✔ group %q added to %s\n", args[1], root)
			return nil
		},
	})

	grp.AddCommand(&cobra.Command{
		Use:     "rm <root> <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete a group from a project root",
		Long: "Delete a group from a project root. The first argument is the root (its\n" +
			"configured full path or just its base name, matched case-insensitively)\n" +
			"and the second is the group to delete.\n\n" +
			"Deleting a group removes the label and ungroups every project that was in\n" +
			"it (reporting how many); the projects and their files are untouched, since\n" +
			"grouping is Hull-side metadata only. Deleting a group that does not exist\n" +
			"is a no-op, so re-running is safe. A running daemon applies the change\n" +
			"live, otherwise it edits groups.yaml directly. To move a single project\n" +
			"out of a group without deleting the group, use hull group mv --clear.",
		Example: "  hull group rm ~/Sites backend\n" +
			"  hull group rm Sites frontend",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			root, err := resolveRoot(a, args[0])
			if err != nil {
				return err
			}
			store, err := a.groupsView(cmd.Context())
			if err != nil {
				return err
			}
			ungrouped := store.RemoveGroup(root, args[1])
			if err := a.saveGroups(cmd.Context(), store); err != nil {
				return err
			}
			if ungrouped > 0 {
				fmt.Printf("✔ group %q removed from %s (%d project(s) ungrouped)\n", args[1], root, ungrouped)
			} else {
				fmt.Printf("✔ group %q removed from %s\n", args[1], root)
			}
			return nil
		},
	})

	grp.AddCommand(&cobra.Command{
		Use:   "order <root> <name...>",
		Short: "Set the group order for a root",
		Long: "Set the display order of groups within a root. The first argument is the\n" +
			"root (full path or base name) and the remaining arguments are group names\n" +
			"in the order you want them shown.\n\n" +
			"The list you pass replaces the root's existing order entirely, so include\n" +
			"every group you want positioned. This affects presentation only (for\n" +
			"example how the GUI lists groups) and does not create or delete groups or\n" +
			"move any project. A running daemon applies the change live, otherwise it\n" +
			"edits groups.yaml directly.",
		Example: "  hull group order ~/Sites backend frontend infra\n" +
			"  hull group order Sites frontend backend",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			root, err := resolveRoot(a, args[0])
			if err != nil {
				return err
			}
			store, err := a.groupsView(cmd.Context())
			if err != nil {
				return err
			}
			store.SetOrder(root, args[1:])
			if err := a.saveGroups(cmd.Context(), store); err != nil {
				return err
			}
			fmt.Printf("✔ group order updated for %s\n", root)
			return nil
		},
	})

	var clear bool
	mv := &cobra.Command{
		Use:   "mv <project> [group]",
		Short: "Move a project into a group (omit group or --clear to ungroup)",
		Long: "Move a project into a group, or ungroup it. Pass the project name and,\n" +
			"optionally, the target group name. Give a group to assign the project to\n" +
			"it (the group need not have been created with hull group add first).\n\n" +
			"To remove a project from its group, either omit the group argument or pass\n" +
			"--clear; passing both a group name and --clear is an error. A running\n" +
			"daemon resolves and applies the change via its per-project endpoint;\n" +
			"otherwise Hull locates the project across configured roots (and\n" +
			"ledger-known clusters) and edits groups.yaml directly. Membership is keyed\n" +
			"by the project's directory, so unmanaged folders can be grouped too.",
		Example: "  hull group mv shop backend\n" +
			"  hull group mv shop\n" +
			"  hull group mv shop --clear",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if len(args) == 2 && clear {
				return fmt.Errorf("pass either a group name or --clear, not both")
			}
			group := ""
			if len(args) == 2 {
				group = args[1]
			}
			// Prefer the daemon's per-project endpoint (resolves the dir for us).
			if client, ok := a.client(); ok {
				if err := client.SetProjectGroup(cmd.Context(), args[0], group); err != nil {
					return err
				}
			} else {
				p, err := a.findProject(args[0])
				if err != nil {
					return err
				}
				store, err := a.groupsView(cmd.Context())
				if err != nil {
					return err
				}
				store.SetMember(p.Dir, group)
				if err := a.saveGroups(cmd.Context(), store); err != nil {
					return err
				}
			}
			if group == "" {
				fmt.Printf("✔ %s ungrouped\n", args[0])
			} else {
				fmt.Printf("✔ %s → %s\n", args[0], group)
			}
			return nil
		},
	}
	mv.Flags().BoolVar(&clear, "clear", false, "remove the project from its group")
	grp.AddCommand(mv)

	rootCmd.AddCommand(grp)
}

// resolveRoot matches a user-supplied root (full path or base name) to a
// configured root, returning the configured path.
func resolveRoot(a *app, arg string) (string, error) {
	clean := filepath.Clean(arg)
	for _, r := range a.Config.Roots {
		if strings.EqualFold(filepath.Clean(r), clean) || strings.EqualFold(filepath.Base(r), arg) {
			return r, nil
		}
	}
	return "", fmt.Errorf("no configured root matches %q (roots: %s)", arg, strings.Join(a.Config.Roots, ", "))
}
