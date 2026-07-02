package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
)

// configInfoT aliases the API config shape the CLI reads/writes.
type configInfoT = api.ConfigInfo

// mutateConfig applies a read-modify-write to the config via the daemon (when
// up) or config.yaml.
func mutateConfig(cmd *cobra.Command, mutate func(*configInfoT)) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	ci, err := a.configView(cmd.Context())
	if err != nil {
		return err
	}
	mutate(&ci)
	if err := a.saveConfig(cmd.Context(), ci); err != nil {
		return err
	}
	fmt.Println("✔ configuration saved")
	return nil
}

func init() {
	cfg := &cobra.Command{
		Use:   "config",
		Short: "View and edit Hull configuration",
		Long: `Read and modify Hull's global configuration (TLD, project roots,
default tools). Changes go through a running daemon when one is up so they
apply live, otherwise they're written straight to config.yaml.`,
	}

	cfg.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Print the current configuration",
		Args:  cobra.NoArgs,
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
				return printJSON(ci)
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintf(w, "tld\t%s\n", ci.TLD)
			_, _ = fmt.Fprintf(w, "defaults.php\t%s\n", dash(ci.Defaults.PHP))
			_, _ = fmt.Fprintf(w, "defaults.editor\t%s\n", dash(ci.Defaults.Editor))
			_, _ = fmt.Fprintf(w, "defaults.db_tool\t%s\n", dash(ci.Defaults.DBTool))
			for i, r := range ci.Roots {
				_, _ = fmt.Fprintf(w, "roots[%d]\t%s\n", i, r)
			}
			return w.Flush()
		},
	})

	cfg.AddCommand(&cobra.Command{
		Use:   "tld <value>",
		Short: "Set the local top-level domain (e.g. test)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateConfig(cmd, func(ci *configInfoT) { ci.TLD = strings.TrimPrefix(args[0], ".") })
		},
	})

	defaultsCmd := &cobra.Command{
		Use:   "defaults <php|editor|db-tool> <value>",
		Short: "Set a default tool/version for new projects",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, val := args[0], args[1]
			switch key {
			case "php", "editor", "db-tool", "db_tool":
			default:
				return fmt.Errorf("unknown default %q (want: php, editor, db-tool)", key)
			}
			return mutateConfig(cmd, func(ci *configInfoT) {
				switch key {
				case "php":
					ci.Defaults.PHP = val
				case "editor":
					ci.Defaults.Editor = val
				case "db-tool", "db_tool":
					ci.Defaults.DBTool = val
				}
			})
		},
		ValidArgs: []string{"php", "editor", "db-tool"},
	}
	cfg.AddCommand(defaultsCmd)

	roots := &cobra.Command{Use: "roots", Short: "Manage project root folders"}
	roots.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured roots",
		Args:    cobra.NoArgs,
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
	})
	roots.AddCommand(&cobra.Command{
		Use:   "add <path>",
		Short: "Add a project root folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
				return fmt.Errorf("%s is not an existing directory", abs)
			}
			return mutateConfig(cmd, func(ci *configInfoT) {
				if !slices.ContainsFunc(ci.Roots, func(r string) bool { return sameRoot(r, abs) }) {
					ci.Roots = append(ci.Roots, abs)
				}
			})
		},
	})
	roots.AddCommand(&cobra.Command{
		Use:   "rm <path>",
		Short: "Remove a project root folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, _ := filepath.Abs(args[0])
			return mutateConfig(cmd, func(ci *configInfoT) {
				ci.Roots = slices.DeleteFunc(ci.Roots, func(r string) bool { return sameRoot(r, abs) || sameRoot(r, args[0]) })
			})
		},
	})
	cfg.AddCommand(roots)

	rootCmd.AddCommand(cfg)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func sameRoot(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
