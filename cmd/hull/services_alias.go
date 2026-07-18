package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/services"
)

// newServicesAliasCommand builds `hull services alias`, which names a shared
// instance so a short name works everywhere an instance name is accepted
// (start/stop/rm and link). Aliases live under services.aliases in config and
// are resolved CLI-side, so a running daemon never needs to know about one.
func newServicesAliasCommand() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "alias [name] [instance]",
		Short: "Name a shared instance so a short name works (e.g. mysql -> mysql-8.4)",
		Long: "Give a shared instance a short, memorable name so you do not have to\n" +
			"type its full versioned name.\n" +
			"\n" +
			"With two arguments it sets an alias: `hull services alias mysql mysql-8.4`\n" +
			"then lets `hull services start mysql`, `hull services stop mysql`, and\n" +
			"`hull link <project> mysql` all resolve to mysql-8.4. With no arguments it\n" +
			"lists the aliases you have set; `hull services alias rm <name>` removes one.\n" +
			"\n" +
			"You often do not even need an alias: if you run only one version of an\n" +
			"engine, the engine name already works (for example `hull services stop\n" +
			"mariadb` finds your sole mariadb instance). Aliases are for when you run\n" +
			"several versions of the same engine and want a stable short name for one.\n" +
			"\n" +
			"An alias is rejected if it collides with a real instance name, and the\n" +
			"target instance must already exist. Aliases pointing at an instance are\n" +
			"dropped automatically when that instance is removed.",
		Example: "  hull services alias mysql mysql-8.4\n" +
			"  hull services alias\n" +
			"  hull services alias rm mysql",
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if remove { // deprecated --rm flag; `hull services alias rm <name>` is preferred
				if len(args) != 1 {
					return fmt.Errorf("usage: hull services alias rm <name>")
				}
				return removeAlias(a, args[0])
			}
			if len(args) == 0 {
				return listAliases(a)
			}
			if len(args) != 2 {
				return fmt.Errorf("usage: hull services alias <name> <instance>")
			}
			return setAlias(a, args[0], args[1])
		},
	}
	cmd.Flags().BoolVar(&remove, "rm", false, "deprecated: use `hull services alias rm <name>`")
	_ = cmd.Flags().MarkHidden("rm")

	cmd.AddCommand(&cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove an alias",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			return removeAlias(a, args[0])
		},
	})
	return cmd
}

// setAlias validates and stores an alias -> instance mapping.
func setAlias(a *app, name, instance string) error {
	mgr := services.NewManager(a.Config)
	if _, err := os.Stat(mgr.Dir(name)); err == nil {
		return fmt.Errorf("%q is already an instance name; choose a different alias", name)
	}
	if _, err := os.Stat(mgr.Dir(instance)); err != nil {
		return fmt.Errorf("no shared instance %q (run `hull services` to see them)", instance)
	}
	return updateAliases(a, func(m map[string]string) error {
		m[name] = instance
		return nil
	})
}

// removeAlias drops an alias, erroring if it does not exist.
func removeAlias(a *app, name string) error {
	return updateAliases(a, func(m map[string]string) error {
		if _, ok := m[name]; !ok {
			return fmt.Errorf("no alias %q", name)
		}
		delete(m, name)
		return nil
	})
}

// listAliases prints the configured aliases as an aligned table.
func listAliases(a *app) error {
	aliases := a.Config.Services.Aliases
	if flagJSON {
		if aliases == nil {
			aliases = map[string]string{}
		}
		return printJSON(aliases)
	}
	if len(aliases) == 0 {
		fmt.Println("No aliases. Set one with: hull services alias <name> <instance>")
		return nil
	}
	names := make([]string, 0, len(aliases))
	for k := range aliases {
		names = append(names, k)
	}
	sort.Strings(names)
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ALIAS\tINSTANCE")
	for _, n := range names {
		fmt.Fprintf(w, "%s\t%s\n", n, aliases[n])
	}
	return w.Flush()
}

// updateAliases applies a mutation to a copy of the alias map and persists it.
// Aliases are a file-only Services setting (like auto_adminer): they write
// config.yaml directly whether or not a daemon is up, and the daemon's config
// PUT reloads the on-disk Services block before saving so this is never
// clobbered.
func updateAliases(a *app, mutate func(map[string]string) error) error {
	aliases := map[string]string{}
	for k, v := range a.Config.Services.Aliases {
		aliases[k] = v
	}
	if err := mutate(aliases); err != nil {
		return err
	}
	if len(aliases) == 0 {
		aliases = nil
	}
	a.Config.Services.Aliases = aliases
	if err := a.Config.Save(); err != nil {
		return err
	}
	fmt.Println("✔ aliases saved")
	return nil
}

// aliasSpec rewrites an explicit alias into the engine@version spec of the
// instance it points at, for commands (link, new) where a bare engine name
// should keep its default-version meaning rather than resolving to whatever
// single instance happens to exist. Non-aliases pass through unchanged.
func aliasSpec(a *app, spec string) string {
	if target, ok := a.Config.Services.Aliases[spec]; ok {
		return services.InstanceToSpec(target)
	}
	return spec
}

// pruneAliasesFor drops any aliases pointing at a removed instance so config
// never dangles. Best-effort and quiet.
func pruneAliasesFor(a *app, instance string) {
	if len(a.Config.Services.Aliases) == 0 {
		return
	}
	aliases := map[string]string{}
	changed := false
	for k, v := range a.Config.Services.Aliases {
		if v == instance {
			changed = true
			continue
		}
		aliases[k] = v
	}
	if !changed {
		return
	}
	if len(aliases) == 0 {
		aliases = nil
	}
	a.Config.Services.Aliases = aliases
	_ = a.Config.Save()
}
