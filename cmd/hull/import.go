package main

import (
	"archive/zip"
	"bufio"
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
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
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
		Long: "Bring an existing application under Hull management with auto-discovery,\n" +
			"or restore a hull-bundle.zip that was exported on another machine.\n" +
			"\n" +
			"A directory import works on the folder you point it at, wherever it\n" +
			"lives, and never moves your code. With no argument it imports the folder\n" +
			"you are in; `hull import <path>` imports that folder in place; a bare\n" +
			"name that is not a local folder is looked up under your parked roots. A\n" +
			"folder outside every root is registered so it stays managed (see also\n" +
			"`hull park` and `hull forget`). Hull inspects composer.json, .env, and\n" +
			"wp-config.php to detect the framework, PHP version, database engine, and\n" +
			"whether Redis is used, then writes a hull.yaml and patches the framework\n" +
			"config (originals are kept as *.hull-backup). Any of --template, --db,\n" +
			"--php, or --redis overrides what was detected.\n" +
			"\n" +
			"For a bundle, pass the .zip path: Hull extracts it into a new directory\n" +
			"under your first root and restores the project from the bundled manifest.\n" +
			"Bundles can declare lifecycle hooks that run commands inside your\n" +
			"containers, so Hull lists them and asks for consent before running an\n" +
			"imported bundle's hooks.\n" +
			"\n" +
			"SQL dumps found in the project (or bundle) can be restored into the\n" +
			"provisioned database; you are offered a picker unless --skip-db.\n" +
			"\n" +
			"Routing: a plain name-based import with no overrides and no --no-start\n" +
			"goes through the daemon (which adopts, starts, and reconciles routes),\n" +
			"then the dump restore runs CLI-side. A .zip, any override flag,\n" +
			"--no-start, or an in-place directory (or current-folder) import forces\n" +
			"the in-process path.",
		Example: "  hull import                 (import the current folder)\n" +
			"  hull import .\\creative       (import a folder where it lives)\n" +
			"  hull import my-old-app --db mysql --php 8.3\n" +
			"  hull import myapp-bundle.zip\n" +
			"  hull import legacy-site --no-start --skip-db",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}

			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			isZip := strings.HasSuffix(strings.ToLower(arg), ".zip")

			// Resolve an in-place import: no argument (or ".") is the current
			// directory; an argument that resolves to an existing directory is
			// that folder, imported where it lives. A bare name that is not a
			// local directory falls through to the classic under-roots lookup.
			if !isZip {
				inPlace := ""
				if arg == "" || arg == "." {
					wd, wderr := os.Getwd()
					if wderr != nil {
						return wderr
					}
					inPlace = wd
				} else if cand, aerr := filepath.Abs(arg); aerr == nil {
					if info, serr := os.Stat(cand); serr == nil && info.IsDir() {
						inPlace = cand
					}
				}
				if inPlace != "" {
					return a.importInPlace(cmd.Context(), inPlace,
						engine.NewOptions{Template: template, DB: db, PHP: php, Redis: redis},
						noStart, skipDumps)
				}
			}
			if arg == "" {
				return fmt.Errorf("nothing to import here; run `hull import` inside a project, or pass a path or a bundle .zip")
			}

			// Daemon-first for the common name-based import so the running
			// daemon adopts and starts the project (and reconciles routes). The
			// daemon endpoint takes no overrides and always starts, so bundle
			// imports, flag overrides, and --no-start stay in-process. The SQL
			// dump restore still runs CLI-side against the started containers.
			hasOverrides := template != "" || db != "" || php != "" || redis
			if client, ok := a.client(); ok && !noStart && !hasOverrides && !isZip {
				job, err := client.Import(cmd.Context(), api.ImportRequest{Name: arg})
				if err != nil {
					return err
				}
				if err := streamJob(cmd.Context(), client, job); err != nil {
					return err
				}
				p, err := a.findProject(arg)
				if err != nil {
					return err
				}
				if p.Manifest == nil {
					return fmt.Errorf("import finished but %s is not managed by Hull", arg)
				}
				if !skipDumps {
					if err := a.offerDumpImport(cmd.Context(), p.Manifest, p.Dir); err != nil {
						return err
					}
				}
				fmt.Printf("✔ %s is up at %s\n", p.Manifest.Name, a.siteURL(p.Manifest.Domain+"."+a.Config.TLD))
				return nil
			}

			var dir string
			var meta *bundle.Meta

			if isZip {
				if len(a.Config.Roots) == 0 {
					return fmt.Errorf("no project root is configured; add one with `hull park` first")
				}
				pre, err := bundle.ReadMeta(arg)
				if err != nil {
					return err
				}
				bm, err := manifest.Parse([]byte(pre.ProjectYAML))
				if err != nil {
					return fmt.Errorf("bundle manifest: %w", err)
				}
				if taken, err := a.findProject(bm.Name); err == nil {
					return fmt.Errorf("a project named %q already exists at %s; rename or remove it before importing this bundle", bm.Name, taken.Dir)
				}
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
					return fmt.Errorf("no project named %q under your roots; if it lives elsewhere, cd into it and run `hull import` (or `hull import <path>`)", arg)
				}
				if p.Manifest != nil {
					return fmt.Errorf("%s already has a hull.yaml, it is managed by Hull", arg)
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
			p := &state.Project{Name: m.Name, Dir: dir, Manifest: m}
			// A manifest Hull generated just now is approved. A bundle's came
			// from somewhere else and can carry hooks, mounts and images, so the
			// user reviews it like any other project Hull did not write.
			if meta == nil {
				if err := a.Engine.Trust(p); err != nil {
					return err
				}
			}

			if noStart {
				fmt.Println("Skipping start (--no-start).")
				return nil
			}
			if err := a.ensureTrusted(p); err != nil {
				return fmt.Errorf("%w; the files are at %s", err, dir)
			}
			if err := a.Engine.Up(cmd.Context(), p); err != nil {
				return err
			}

			if !skipDumps {
				if err := a.offerDumpImport(cmd.Context(), m, dir); err != nil {
					return err
				}
			}
			fmt.Printf("✔ %s is up at %s\n", m.Name, a.siteURL(m.Domain+"."+a.Config.TLD))
			return nil
		},
	}
	cmd.Flags().StringVar(&template, "template", "", "override detected template")
	cmd.Flags().StringVar(&db, "db", "", "override detected database engine")
	cmd.Flags().StringVar(&php, "php", "", "override detected PHP version")
	cmd.Flags().BoolVar(&redis, "redis", false, "add Redis even if not detected")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "import without booting")
	cmd.Flags().BoolVar(&skipDumps, "skip-db", false, "do not offer SQL dump restore")
	cmd.Flags().BoolVar(&skipDumps, "skip-dumps", false, "deprecated alias for --skip-db")
	_ = cmd.Flags().MarkHidden("skip-dumps")
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

// importInPlace imports the project living at dir, wherever that is, without
// moving it: it detects the framework, writes hull.yaml, registers the
// directory when it is outside every parked root (so it stays findable by name,
// in `hull list`, and when you cd into it), and starts it.
func (a *app) importInPlace(ctx context.Context, dir string, overrides engine.NewOptions, noStart, skipDumps bool) error {
	name := filepath.Base(dir)

	// Already a Hull project (for example a freshly cloned repo): just register
	// and start it, do not re-adopt.
	if fileExists(filepath.Join(dir, manifest.Filename)) {
		m, err := manifest.Load(dir)
		if err != nil {
			return fmt.Errorf("%s has a hull.yaml but it failed to parse: %w", name, err)
		}
		if err := a.registerProject(ctx, dir); err != nil {
			return err
		}
		fmt.Printf("✔ %s registered (%s)\n", m.Name, dir)
		if noStart {
			return nil
		}
		p := &state.Project{Name: m.Name, Dir: dir, Manifest: m}
		if err := a.ensureTrusted(p); err != nil {
			return err
		}
		if err := a.Engine.Up(ctx, p); err != nil {
			return err
		}
		fmt.Printf("✔ %s is up at %s\n", m.Name, a.siteURL(m.Domain+"."+a.Config.TLD))
		return nil
	}

	// The folder looks like it holds several projects. This is only a heuristic
	// (an unusual single-project layout, e.g. a multi-site PHP app, can trip it),
	// so warn and ask rather than refuse. On confirmation the whole folder is
	// imported as one project.
	if looksLikeProjectFolder(dir) {
		fmt.Printf("! %s looks like it contains multiple projects; parking it with `hull park` usually fits better.\n", dir)
		ok, cerr := confirm("Import the whole folder as a single project anyway?")
		if cerr != nil {
			return fmt.Errorf("%s looks like a folder of projects; park it with `hull park` (from inside it), or re-run with --yes to import it as one project", dir)
		}
		if !ok {
			return fmt.Errorf("import cancelled; park %s with `hull park` instead", dir)
		}
	}

	// When no template was pinned with --template, let the user choose the type
	// to import as (defaulting to what detection found). Off a terminal this
	// falls back to detection so scripts are unaffected.
	if overrides.Template == "" {
		t, terr := chooseImportTemplate(dir)
		if terr != nil {
			return terr
		}
		overrides.Template = t
	}

	m, err := resolveImportManifest(name, dir, nil, overrides)
	if err != nil {
		return err
	}
	fmt.Printf("Importing %s as %s (db: %s) in place at %s\n", m.Name, m.Template, describeDB(m), dir)
	if err := a.Engine.Adopt(m, dir); err != nil {
		return err
	}
	fmt.Println("✔ hull.yaml written, framework config patched (backups: *.hull-backup)")
	if err := a.Engine.Trust(&state.Project{Name: m.Name, Dir: dir, Manifest: m}); err != nil {
		return err
	}
	if err := a.registerProject(ctx, dir); err != nil {
		return err
	}
	if noStart {
		fmt.Println("Skipping start (--no-start).")
		return nil
	}
	p := &state.Project{Name: m.Name, Dir: dir, Manifest: m}
	if err := a.Engine.Up(ctx, p); err != nil {
		return err
	}
	if !skipDumps {
		if err := a.offerDumpImport(ctx, m, dir); err != nil {
			return err
		}
	}
	fmt.Printf("✔ %s is up at %s\n", m.Name, a.siteURL(m.Domain+"."+a.Config.TLD))
	return nil
}

// registerProject persists dir in the projects list (daemon-aware) unless it
// already lives under a parked root or is already registered. It also updates
// the in-memory config so a later lookup in this same process resolves it.
func (a *app) registerProject(ctx context.Context, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, r := range a.Config.Roots {
		if state.Under(r, abs) {
			return nil // already covered by a parked root
		}
	}
	ci, err := a.configView(ctx)
	if err != nil {
		return err
	}
	for _, p := range ci.Projects {
		if sameRoot(p, abs) {
			return nil // already registered
		}
	}
	ci.Projects = append(ci.Projects, abs)
	a.Config.Projects = ci.Projects
	return a.saveConfig(ctx, ci)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// looksLikeProjectFolder reports whether dir is a parent-of-projects (park
// territory) rather than a single project (import territory): it has no app or
// manifest markers of its own but contains child directories that do.
func looksLikeProjectFolder(dir string) bool {
	for _, f := range []string{manifest.Filename, "composer.json", "package.json", "index.php", "wp-config.php", "artisan", "Dockerfile", "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if fileExists(filepath.Join(dir, f)) {
			return false
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		for _, f := range []string{manifest.Filename, "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml", "composer.json", "index.php"} {
			if fileExists(filepath.Join(dir, e.Name(), f)) {
				return true
			}
		}
	}
	return false
}

// chooseImportTemplate returns the PHP site template to import a folder as. It
// prompts interactively (defaulting to what detection found) but falls back to
// detection when --yes is set or there is no terminal, so scripts never hang.
func chooseImportTemplate(dir string) (string, error) {
	detected := bundle.Detect(dir).Template
	if detected == "" {
		detected = "plain"
	}
	if flagYes || !isInteractive() {
		return detected, nil
	}
	return pickOne("Import as which type?", orderedTemplates(detected))
}

// orderedTemplates lists the PHP site templates with the detected one first, so
// pressing enter in the picker accepts the detection.
func orderedTemplates(detected string) []string {
	all := []string{"plain", "laravel", "wordpress"}
	known := false
	for _, t := range all {
		if t == detected {
			known = true
		}
	}
	if !known {
		return all
	}
	out := []string{detected}
	for _, t := range all {
		if t != detected {
			out = append(out, t)
		}
	}
	return out
}

func describeDB(m *manifest.Manifest) string {
	if _, db, ok := m.DatabaseService(); ok {
		return db.Engine
	}
	return "none"
}

// offerDumpImport finds SQL dumps in the project, lets the user pick one, and
// loads it into the project's first database.
func (a *app) offerDumpImport(ctx context.Context, m *manifest.Manifest, dir string) error {
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
	if err := a.restoreDump(ctx, m, dir, dbKey, filepath.Join(dir, choice)); err != nil {
		return fmt.Errorf("%w; load it later with: hull db import %s %s", err, m.Name, choice)
	}
	return nil
}

// restoreDump loads a dump file into one of a project's database services. It
// refuses a dump from the other kind of database before writing anything,
// waits for the database, and filters the dump so it lands in this database
// as the project's own login.
func (a *app) restoreDump(ctx context.Context, m *manifest.Manifest, dir, key, file string) error {
	svc := m.Services[key]
	in, closer, err := openCheckedDump(file, svc.Engine)
	if err != nil {
		return err
	}
	defer closer()

	appLogin, adminLogin := a.Engine.DBLogins(m, key)
	fmt.Println("Waiting for the database...")
	ready, err := bundle.ReadyCommand(m, key, dir, adminLogin)
	if err != nil {
		return err
	}
	if !waitReady(ctx, ready) {
		return fmt.Errorf("database %s is not answering; start it with %s", key, startHint(m, key))
	}
	if svc.Engine == "mysql" || svc.Engine == "mariadb" {
		undo, err := trustRoutines(ctx, m, dir, key, adminLogin)
		if err != nil {
			return err
		}
		defer undo()
	}

	restore, err := bundle.RestoreCommand(m, key, dir, appLogin)
	if err != nil {
		return err
	}
	fmt.Printf("Loading %s into %s...\n", filepath.Base(file), key)
	pr, pw := io.Pipe()
	go func() {
		_ = pw.CloseWithError(bundle.FilterDump(in, pw, svc.Engine))
	}()
	if err := dockerx.ExecStdin(ctx, restore.Dir, pr, restore.Name, restore.Args...); err != nil {
		_ = pr.CloseWithError(err)
		return fmt.Errorf("loading %s: %w", filepath.Base(file), err)
	}
	fmt.Println("✔ Database loaded.")
	return nil
}

// openCheckedDump opens a dump and refuses one that the engine cannot load,
// before anything is written to the database.
func openCheckedDump(file, engine string) (*bufio.Reader, func(), error) {
	src, closer, err := openDump(file)
	if err != nil {
		return nil, nil, err
	}
	in := bufio.NewReaderSize(src, 64<<10)
	head, _ := in.Peek(16 << 10)
	if err := bundle.CheckDumpEngine(head, engine); err != nil {
		closer()
		return nil, nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	return in, closer, nil
}

// trustRoutines sets log_bin_trust_function_creators for a restore and returns
// how to put the server back. While binary logging is on (MySQL's default) and
// it is 0, MySQL refuses a dumped stored function that does not declare
// DETERMINISTIC, NO SQL or READS SQL DATA, even for root, and every function
// and trigger from a login without SUPER.
func trustRoutines(ctx context.Context, m *manifest.Manifest, dir, key string, admin bundle.Auth) (func(), error) {
	run := func(ctx context.Context, sql string) (string, error) {
		c, err := bundle.MySQLCommand(m, key, dir, admin, sql)
		if err != nil {
			return "", err
		}
		return dockerx.Output(ctx, c.Dir, c.Name, c.Args...)
	}
	out, err := run(ctx, "SELECT @@GLOBAL.log_bin_trust_function_creators")
	if err != nil {
		return nil, fmt.Errorf("reading log_bin_trust_function_creators in %s: %w", key, err)
	}
	if strings.TrimSpace(out) == "1" {
		return func() {}, nil
	}
	if _, err := run(ctx, "SET GLOBAL log_bin_trust_function_creators = 1"); err != nil {
		return nil, fmt.Errorf("letting %s create stored routines: %w", key, err)
	}
	return func() {
		// Put back even after Ctrl-C, which has cancelled ctx by then.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = run(ctx, "SET GLOBAL log_bin_trust_function_creators = 0")
	}, nil
}

// startHint is the command that starts a project's database service.
func startHint(m *manifest.Manifest, key string) string {
	if s := m.Services[key]; s != nil && s.Mode == manifest.ModeShared {
		return "`hull services start " + templates.InstanceName(s.Engine, s.Version) + "`"
	}
	return "`hull up " + m.Name + "`"
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
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".gz"):
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
	case strings.HasSuffix(lower, ".zip"):
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range zr.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".sql") {
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
