package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/dockerx"
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
		Long: `Bundle a project for another machine: code (vendor/node_modules/.git
excluded by default), a fresh database dump from its running database, and
its .env with secret values stripped (use --include-env to keep them).

Import the result anywhere with: hull import <name>-bundle.zip`,
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
				return fmt.Errorf("%s is a legacy v1 project , adopt it first with: hull migrate %s", p.Name, p.Name)
			}
			m := p.Manifest

			var dumpKeys []string
			if !skipDB {
				for _, key := range m.ServiceKeys() {
					if _, err := bundle.DumpCommand(m, key, p.Dir); err == nil {
						dumpKeys = append(dumpKeys, key)
					}
				}
				if len(dumpKeys) > 0 {
					if err := dockerx.EngineCheck(cmd.Context()); err != nil {
						return fmt.Errorf("database dump needs the engine (or pass --skip-db): %w", err)
					}
				}
			}

			manifestData, err := os.ReadFile(p.Dir + "/hull.yaml")
			if err != nil {
				return err
			}

			target := out
			if target == "" {
				target = p.Name + "-bundle.zip"
			}
			f, err := os.Create(target)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()

			fmt.Printf("Exporting %s -> %s\n", p.Name, target)
			meta, err := bundle.WriteBundle(f, bundle.ExportOptions{
				ProjectDir:  p.Dir,
				ProjectYAML: string(manifestData),
				HullVersion: version.String(),
				IncludeEnv:  includeEnv,
				KeepVendor:  includeVendor,
				DumpKeys:    dumpKeys,
				DumpDB: func(key string, w io.Writer) error {
					dump, err := bundle.DumpCommand(m, key, p.Dir)
					if err != nil {
						return err
					}
					fmt.Printf("  dumping %s (%s)...\n", key, m.Services[key].Engine)
					return dockerx.ExecCapture(cmd.Context(), dump.Dir, w, dump.Name, dump.Args...)
				},
			})
			if err != nil {
				_ = os.Remove(target)
				return err
			}
			if hint := bundle.StrippedEnvHint(meta); hint != "" {
				fmt.Println("!", hint, "(--include-env keeps them)")
			}
			info, _ := os.Stat(target)
			fmt.Printf("✔ Bundle written: %s (%.1f MB)\n", target, float64(info.Size())/1024/1024)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output path (default <name>-bundle.zip)")
	cmd.Flags().BoolVar(&includeEnv, "include-env", false, "keep secret values in the bundled .env")
	cmd.Flags().BoolVar(&includeVendor, "include-vendor", false, "include vendor/ and node_modules/")
	cmd.Flags().BoolVar(&skipDB, "skip-db", false, "skip database dumps")
	rootCmd.AddCommand(cmd)
}
