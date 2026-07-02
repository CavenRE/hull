package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	grp := &cobra.Command{
		Use:   "group",
		Short: "Organize projects into virtual groups",
		Long: `Virtual groups are organizational labels shown inside each project
root. They are stored Hull-side (groups.yaml) keyed by project path , nothing
in your project folders changes, and unmanaged folders can be grouped too.`,
	}

	grp.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List groups and members",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			for root, rg := range store.Roots {
				fmt.Printf("%s\n", root)
				for _, g := range rg.Groups {
					fmt.Printf("  %s\n", g)
				}
			}
			if len(store.Members) > 0 {
				fmt.Println("members:")
				for dir, g := range store.Members {
					fmt.Printf("  %s → %s\n", dir, g)
				}
			}
			return nil
		},
	})

	grp.AddCommand(&cobra.Command{
		Use:   "add <root> <name>",
		Short: "Create a group inside a project root",
		Args:  cobra.ExactArgs(2),
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
		Use:   "order <root> <name...>",
		Short: "Set the group order for a root",
		Args:  cobra.MinimumNArgs(2),
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
		Args:  cobra.RangeArgs(1, 2),
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
