package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/version"
)

func init() {
	var (
		out           string
		includeEnv    bool
		includeVendor bool
		skipDB        bool
	)
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a project as a portable hull-bundle.zip",
		Long: "Package a managed project into a single portable hull-bundle.zip that can\n" +
			"be moved to another machine and restored with hull import.\n" +
			"\n" +
			"The bundle contains the project code (without .git, and by default without\n" +
			"vendor and node_modules at the project root), the hull.yaml manifest, a\n" +
			"fresh SQL dump taken from each running database, and the project's .env\n" +
			"with secret-looking values blanked (passwords, keys, tokens, URLs with a\n" +
			"password, PEM blocks). Other .env.* files, Hull's *.hull-backup copies and\n" +
			"symbolic links are left out. Secrets in other files, such as wp-config.php,\n" +
			"and the data in the dumps travel as they are, so keep a bundle private.\n" +
			"\n" +
			"Flags let you widen what goes in: --include-env keeps the real .env values\n" +
			"and the other .env.* files, --include-vendor ships vendor/ and\n" +
			"node_modules/, and --skip-db omits the database dumps entirely. Taking\n" +
			"dumps requires Docker to be running (the project must be up); use\n" +
			"--skip-db if the engine is down.\n" +
			"\n" +
			"This runs in-process only; the project must already be managed by Hull\n" +
			"(have a hull.yaml). The output path defaults to <name>-bundle.zip; use\n" +
			"--output to write elsewhere.\n" +
			"\n" +
			"Restore the result anywhere with: hull import <name>-bundle.zip",
		Example: "  hull export shop\n" +
			"  hull export shop -o /backups/shop.zip\n" +
			"  hull export shop --include-vendor --include-env\n" +
			"  hull export shop --skip-db",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			if p.Manifest == nil {
				return fmt.Errorf("%s is a legacy v1 project: adopt it first with: hull migrate %s", p.Name, p.Name)
			}
			m := p.Manifest

			var dumpKeys []string
			if !skipDB {
				for _, key := range m.ServiceKeys() {
					if _, err := bundle.DumpCommand(m, key, p.Dir, bundle.Auth{}); err == nil {
						dumpKeys = append(dumpKeys, key)
					}
				}
			}

			manifestData, err := os.ReadFile(filepath.Join(p.Dir, manifest.Filename))
			if err != nil {
				return err
			}

			target := out
			if target == "" {
				target = p.Name + "-bundle.zip"
			}
			// Write beside the target and rename at the end, so a failed export
			// never truncates or removes an older bundle. The -bundle.zip suffix
			// keeps the walk from packing the partial file into itself.
			f, err := os.CreateTemp(filepath.Dir(target), ".hull-export-*-bundle.zip")
			if err != nil {
				return err
			}
			tmp := f.Name()
			defer func() { _ = f.Close(); _ = os.Remove(tmp) }()

			fmt.Printf("Exporting %s -> %s\n", p.Name, target)
			meta, err := bundle.WriteBundle(f, bundle.ExportOptions{
				ProjectDir:  p.Dir,
				ProjectYAML: string(manifestData),
				HullVersion: version.String(),
				IncludeEnv:  includeEnv,
				KeepVendor:  includeVendor,
				KeepCompose: m.Type == manifest.TypeCluster,
				DumpKeys:    dumpKeys,
				DumpDB: func(key string, w io.Writer) error {
					_, admin := a.Engine.DBLogins(m, key)
					dump, err := bundle.DumpCommand(m, key, p.Dir, admin)
					if err != nil {
						return err
					}
					fmt.Printf("  dumping %s (%s)...\n", key, m.Services[key].Engine)
					return dockerx.ExecCapture(cmd.Context(), dump.Dir, w, dump.Name, dump.Args...)
				},
			})
			if err != nil {
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := os.Rename(tmp, target); err != nil {
				return err
			}
			if hint := bundle.StrippedEnvHint(meta); hint != "" {
				fmt.Println("!", hint, "(--include-env keeps them)")
			}
			if len(meta.Skipped) > 0 {
				fmt.Println("! Left out (links, Hull backups, env files):", strings.Join(meta.Skipped, ", "))
			}
			info, _ := os.Stat(target)
			fmt.Printf("✔ Bundle written: %s (%.1f MB)\n", target, float64(info.Size())/1024/1024)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output path (default <name>-bundle.zip)")
	cmd.Flags().BoolVar(&includeEnv, "include-env", false, "keep the real .env values and the other .env.* files")
	cmd.Flags().BoolVar(&includeVendor, "include-vendor", false, "include vendor/ and node_modules/")
	cmd.Flags().BoolVar(&skipDB, "skip-db", false, "skip database dumps")
	rootCmd.AddCommand(cmd)
}
