package main

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/manifest"
)

func init() {
	var (
		template  string
		db        string
		php       string
		redis     bool
		noStart   bool
		skipDumps bool
	)
	cmd := &cobra.Command{
		Use:   "import <name|bundle.zip>",
		Short: "Import an existing project or a Hull bundle",
		Long: `Import an existing application with auto-discovery, or restore a
hull-bundle.zip exported on another machine.

For a directory import, move the code to <root>/<name> first. Hull detects
the framework, PHP version, database, and Redis from composer.json, .env,
and wp-config.php; flags override detection. Found SQL dumps can be
restored into the provisioned database.`,
		Example: `  hull import my-old-app
  hull import my-old-app --db mysql
  hull import myapp-bundle.zip`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}

			// Daemon-first for the common name-based import so the running
			// daemon adopts and starts the project (and reconciles routes). The
			// daemon endpoint takes no overrides and always starts, so bundle
			// imports, flag overrides, and --no-start stay in-process. The SQL
			// dump restore still runs CLI-side against the started containers.
			hasOverrides := template != "" || db != "" || php != "" || redis
			if client, ok := a.client(); ok && !noStart && !hasOverrides && !strings.HasSuffix(args[0], ".zip") {
				job, err := client.Import(cmd.Context(), api.ImportRequest{Name: args[0]})
				if err != nil {
					return err
				}
				if err := streamJob(cmd.Context(), client, job); err != nil {
					return err
				}
				p, err := a.findProject(args[0])
				if err != nil {
					return err
				}
				if p.Manifest == nil {
					return fmt.Errorf("import finished but %s is not managed by Hull", args[0])
				}
				if !skipDumps {
					if err := offerDumpImport(cmd.Context(), p.Manifest, p.Dir); err != nil {
						return err
					}
				}
				fmt.Printf("✔ %s is up at https://%s.%s\n", p.Manifest.Name, p.Manifest.Domain, a.Config.TLD)
				return nil
			}

			if !noStart {
				if err := dockerx.EngineCheck(cmd.Context()); err != nil {
					return err
				}
			}

			arg := args[0]
			var dir string
			var meta *bundle.Meta

			if strings.HasSuffix(arg, ".zip") {
				baseName := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(arg), ".zip"), "-bundle")
				dir = filepath.Join(a.Config.Roots[0], baseName)
				if _, err := os.Stat(dir); err == nil {
					return fmt.Errorf("target directory %s already exists", dir)
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				fmt.Printf("Extracting bundle into %s...\n", dir)
				meta, err = bundle.Extract(arg, dir)
				if err != nil {
					return err
				}
				if hint := bundle.StrippedEnvHint(meta); hint != "" {
					fmt.Println("!", hint)
				}
			} else {
				p, err := a.findProject(arg)
				if err != nil {
					return fmt.Errorf("move your project to %s first (%w)", filepath.Join(a.Config.Roots[0], arg), err)
				}
				if p.Manifest != nil {
					return fmt.Errorf("%s already has a hull.yaml , it is managed by Hull", arg)
				}
				dir = p.Dir
			}

			m, err := resolveImportManifest(filepath.Base(dir), dir, meta, engine.NewOptions{
				Template: template, DB: db, PHP: php, Redis: redis,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Importing %s as %s (db: %s)\n", m.Name, m.Template, describeDB(m))

			if err := a.Engine.Adopt(m, dir); err != nil {
				return err
			}
			fmt.Println("✔ hull.yaml written, framework config patched (backups: *.hull-backup)")

			if noStart {
				fmt.Println("Skipping start (--no-start).")
				return nil
			}
			p, err := a.findProject(m.Name)
			if err != nil {
				return err
			}
			// A bundle's manifest can carry lifecycle hooks that run arbitrary
			// commands inside containers on the next `up`. Never run a
			// downloaded bundle's hooks without explicit consent.
			if meta != nil {
				if hooks := bundleHookSummary(m); len(hooks) > 0 {
					fmt.Println("This bundle defines lifecycle hooks that will run commands inside your containers:")
					for _, h := range hooks {
						fmt.Println("  -", h)
					}
					ok, err := confirm("Trust and run this bundle's hooks?")
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("import aborted , edit %s to remove the hooks, then re-run", filepath.Join(dir, manifest.Filename))
					}
				}
			}
			if err := a.Engine.Up(cmd.Context(), p); err != nil {
				return err
			}

			if !skipDumps {
				if err := offerDumpImport(cmd.Context(), m, dir); err != nil {
					return err
				}
			}
			fmt.Printf("✔ %s is up at https://%s.%s\n", m.Name, m.Domain, a.Config.TLD)
			return nil
		},
	}
	cmd.Flags().StringVar(&template, "template", "", "override detected template")
	cmd.Flags().StringVar(&db, "db", "", "override detected database engine")
	cmd.Flags().StringVar(&php, "php", "", "override detected PHP version")
	cmd.Flags().BoolVar(&redis, "redis", false, "add Redis even if not detected")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "import without booting")
	cmd.Flags().BoolVar(&skipDumps, "skip-dumps", false, "do not offer SQL dump restore")
	rootCmd.AddCommand(cmd)
}

// resolveImportManifest prefers a bundle's own manifest; otherwise builds
// one from auto-detection plus flag overrides.
func resolveImportManifest(name, dir string, meta *bundle.Meta, overrides engine.NewOptions) (*manifest.Manifest, error) {
	if meta != nil {
		return manifest.Parse([]byte(meta.ProjectYAML))
	}
	det := bundle.Detect(dir)
	return engine.BuildImportManifest(name, det, overrides)
}

// bundleHookSummary lists every lifecycle hook a manifest declares, as
// "event: command", for the trust prompt on bundle import.
func bundleHookSummary(m *manifest.Manifest) []string {
	var out []string
	add := func(event string, hs []manifest.Hook) {
		for _, h := range hs {
			out = append(out, event+": "+h.Run)
		}
	}
	add("post_create", m.Hooks.PostCreate)
	add("post_import", m.Hooks.PostImport)
	add("pre_up", m.Hooks.PreUp)
	add("post_up", m.Hooks.PostUp)
	add("pre_down", m.Hooks.PreDown)
	add("post_rebuild", m.Hooks.PostRebuild)
	add("post_reset", m.Hooks.PostReset)
	return out
}

func describeDB(m *manifest.Manifest) string {
	if _, db, ok := m.DatabaseService(); ok {
		return db.Engine
	}
	return "none"
}

// offerDumpImport finds SQL dumps in the project, lets the user pick one,
// waits for the database, and restores it , the Go port of v1's flow.
func offerDumpImport(ctx context.Context, m *manifest.Manifest, dir string) error {
	dbKey, _, hasDB := m.DatabaseService()
	dumps := bundle.FindDumps(dir)
	if !hasDB || len(dumps) == 0 {
		return nil
	}

	options := make([]string, 0, len(dumps)+1)
	for _, d := range dumps {
		options = append(options, filepath.Base(d))
	}
	options = append(options, "Skip import")
	choice, err := pickOne("Restore a database dump?", options)
	if err != nil || choice == "Skip import" || choice == "" {
		return err
	}
	dumpPath := filepath.Join(dir, choice)

	fmt.Println("Waiting for the database engine...")
	ready, err := bundle.ReadyCommand(m, dbKey, dir)
	if err != nil {
		return err
	}
	if !waitReady(ctx, ready) {
		fmt.Println("! Database did not become ready in time; attempting restore anyway.")
	}

	fmt.Printf("Restoring %s into %s...\n", choice, dbKey)
	restore, err := bundle.RestoreCommand(m, dbKey, dir)
	if err != nil {
		return err
	}
	src, closer, err := openDump(dumpPath)
	if err != nil {
		return err
	}
	defer closer()

	pr, pw := io.Pipe()
	go func() {
		_ = pw.CloseWithError(bundle.FilterDump(src, pw))
	}()
	if err := dockerx.ExecStdin(ctx, restore.Dir, pr, restore.Name, restore.Args...); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}
	fmt.Println("✔ Database restored.")
	return nil
}

func waitReady(ctx context.Context, probe bundle.Cmd) bool {
	for i := 0; i < 30; i++ {
		if _, err := dockerx.Output(ctx, probe.Dir, probe.Name, probe.Args...); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return false
}

// openDump opens .sql, .sql.gz, or .zip (first .sql entry) dump files.
func openDump(path string) (io.Reader, func(), error) {
	switch {
	case strings.HasSuffix(path, ".gz"):
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		gz, err := gzip.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, nil, err
		}
		return gz, func() { _ = gz.Close(); _ = f.Close() }, nil
	case strings.HasSuffix(path, ".zip"):
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range zr.File {
			if strings.HasSuffix(f.Name, ".sql") {
				rc, err := f.Open()
				if err != nil {
					_ = zr.Close()
					return nil, nil, err
				}
				return rc, func() { _ = rc.Close(); _ = zr.Close() }, nil
			}
		}
		_ = zr.Close()
		return nil, nil, fmt.Errorf("no .sql file inside %s", filepath.Base(path))
	default:
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		return f, func() { _ = f.Close() }, nil
	}
}
