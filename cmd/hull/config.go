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
		Long: "Read and modify Hull's global configuration: the local TLD, the project\n" +
			"root folders Hull scans, and default tools/versions for new projects.\n\n" +
			"Every subcommand routes through a running daemon when one is up (so changes\n" +
			"apply live to the running router and DNS), and falls back to editing the\n" +
			"config.yaml file in your Hull home directory when no daemon is reachable.\n\n" +
			"Use the subcommands to inspect (get, roots list) or change (tld, defaults,\n" +
			"roots add/rm) settings. With no subcommand it prints the current config.",
		Example: "  hull config get\n" +
			"  hull config tld test\n" +
			"  hull config roots add ~/Sites",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runConfigGet(cmd) },
	}

	cfg.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Print the current configuration",
		Long: "Print the effective Hull configuration: the local TLD, the configured\n" +
			"project roots, and the default PHP version, editor, and database tool.\n\n" +
			"When a daemon is running this reads the live config it holds in memory\n" +
			"(GET /v1/config); otherwise it reads config.yaml from your Hull home\n" +
			"directory directly. This is a read-only command and never writes anything.\n\n" +
			"By default it prints an aligned key/value table (unset defaults show as a\n" +
			"single dash). Add --json for a machine-readable object with tld, roots, and\n" +
			"a nested defaults block (php, editor, db_tool).",
		Example: "  hull config get\n" +
			"  hull config get --json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runConfigGet(cmd) },
	})

	cfg.AddCommand(&cobra.Command{
		Use:   "tld <value>",
		Short: "Set the local top-level domain (e.g. test)",
		Long: "Set the local top-level domain that Hull uses to build project URLs, for\n" +
			"example test or local. Projects are then served at names like\n" +
			"myapp.<tld>. Pass the value without a leading dot; a leading dot is\n" +
			"stripped automatically, so tld .test and tld test are equivalent.\n\n" +
			"This is a read-modify-write: Hull reads the current config, updates only\n" +
			"the tld field, and writes it back. A running daemon applies the change\n" +
			"live to routing and DNS; with no daemon it edits config.yaml directly.\n\n" +
			"Note that changing the TLD changes every project's URL. After changing it\n" +
			"you may need to re-run hull setup so DNS and certificates cover the new\n" +
			"suffix.",
		Example: "  hull config tld test\n" +
			"  hull config tld local",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return mutateConfig(cmd, func(ci *configInfoT) { ci.TLD = strings.TrimPrefix(args[0], ".") })
		},
	})

	defaultsCmd := &cobra.Command{
		Use:   "defaults <php|editor|db-tool> <value>",
		Short: "Set a default tool/version for new projects",
		Long: "Set a default tool or version that new projects inherit. The first\n" +
			"argument is the key and the second is its value:\n\n" +
			"  php       the default PHP version for scaffolded projects (e.g. 8.3)\n" +
			"  editor    the editor command used when Hull opens a project\n" +
			"  db-tool   the database GUI/tool Hull points you at\n\n" +
			"Any other key is rejected. This is a read-modify-write against the config:\n" +
			"a running daemon applies it live, otherwise it edits config.yaml. Defaults\n" +
			"only affect projects created after the change; existing projects keep the\n" +
			"values recorded in their own hull.yaml.",
		Example: "  hull config defaults php 8.3\n" +
			"  hull config defaults editor code\n" +
			"  hull config defaults db-tool tableplus",
		Args: cobra.ExactArgs(2),
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

	roots := &cobra.Command{
		Use:   "roots",
		Short: "Manage project root folders",
		Long: "Manage the list of root folders Hull scans for projects. Every managed\n" +
			"project lives under one of these roots, and commands like hull list and\n" +
			"hull group operate per root.\n\n" +
			"Use the subcommands to inspect (list) or change (add, rm) the set of roots.\n" +
			"All three route through a running daemon when one is up, otherwise they\n" +
			"read or write config.yaml directly. With no subcommand it lists the roots.",
		Example: "  hull config roots list\n" +
			"  hull config roots add ~/Sites\n" +
			"  hull config roots rm ~/old-projects",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runRootsList(cmd) },
	}
	roots.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured roots",
		Long: "List the project root folders Hull is configured to scan, one absolute\n" +
			"path per line. These are the folders under which hull list, hull new, and\n" +
			"the group commands look for projects.\n\n" +
			"When a daemon is running this reads its live config, otherwise it reads\n" +
			"config.yaml directly. It is read-only. Add --json to emit a JSON array of\n" +
			"path strings for scripting.",
		Example: "  hull config roots list\n" +
			"  hull config roots list --json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runRootsList(cmd) },
	})
	roots.AddCommand(&cobra.Command{
		Use:   "add <path>",
		Short: "Add a project root folder",
		Long: "Add a folder to Hull's list of project roots so Hull scans it for\n" +
			"projects. The path may be relative; it is resolved to an absolute path\n" +
			"before being stored.\n\n" +
			"The folder must already exist and be a directory, otherwise the command\n" +
			"fails with an error. Adding a root that is already configured is a no-op\n" +
			"(comparison is case-insensitive on the cleaned path), so re-running is\n" +
			"safe. A running daemon applies the change live, otherwise it edits\n" +
			"config.yaml directly.",
		Example: "  hull config roots add ~/Sites\n" +
			"  hull config roots add .",
		Args: cobra.ExactArgs(1),
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
		Long: "Remove a folder from Hull's list of project roots. Hull stops scanning\n" +
			"it, but the folder and any projects inside it are left completely\n" +
			"untouched on disk; only Hull's configured list changes.\n\n" +
			"Unlike add, rm does not require the path to still exist, so you can prune\n" +
			"a root whose folder has already been deleted. The argument may be the full\n" +
			"absolute path or just the folder's base name; matching is case-insensitive\n" +
			"on the cleaned path. A running daemon applies the change live, otherwise it\n" +
			"edits config.yaml directly.",
		Example: "  hull config roots rm ~/Sites\n" +
			"  hull config roots rm old-projects",
		Args: cobra.ExactArgs(1),
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

// runConfigGet prints the effective config, shared by `config get` and a bare
// `hull config`.
func runConfigGet(cmd *cobra.Command) error {
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
}

// runRootsList prints the configured roots, shared by `config roots list` and a
// bare `hull config roots`.
func runRootsList(cmd *cobra.Command) error {
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
