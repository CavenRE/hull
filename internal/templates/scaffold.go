package templates

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	_ "embed"
)

//go:embed assets/plain-index.php
var plainIndex string

//go:embed assets/xdebug.ini
var xdebugINI string

//go:embed assets/opcache.ini
var opcacheINI string

//go:embed assets/hull-composer-install.sh
var composerInstallSH string

//go:embed assets/hull-fix-perms.sh
var fixPermsSH string

//go:embed assets/hull-login.php
var adminerLogin string

//go:embed assets/static-index.html
var staticIndex string

//go:embed assets/python-app.py
var pythonApp string

//go:embed assets/node-server.js
var nodeServer string

//go:embed assets/go-main.txt
var goMain string

//go:embed assets/air.toml
var airToml string

// EnsureSystemFiles writes Hull-owned support files (the shared opcache.ini and
// xdebug.ini every PHP container mounts, Adminer's auto-login plugin) into the
// Hull home directory if missing, so a fresh v2 machine works without a v1
// installation. Existing files are left untouched , they may carry user tweaks.
func EnsureSystemFiles(hullHome string) error {
	type sysFile struct {
		content string
		mode    os.FileMode
	}
	files := map[string]sysFile{
		filepath.Join(hullHome, "system", "php", "opcache.ini"):              {opcacheINI, 0o644},
		filepath.Join(hullHome, "system", "php", "xdebug.ini"):               {xdebugINI, 0o644},
		filepath.Join(hullHome, "system", "php", "hull-composer-install.sh"): {composerInstallSH, 0o755},
		filepath.Join(hullHome, "system", "php", "hull-fix-perms.sh"):        {fixPermsSH, 0o755},
		filepath.Join(hullHome, "system", "adminer", "hull-login.php"):       {adminerLogin, 0o644},
	}
	for target, f := range files {
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(f.content), f.mode); err != nil {
			return err
		}
	}
	return nil
}

// AdminerPluginPath is where the auto-login plugin lives in the Hull home.
func AdminerPluginPath(hullHome string) string {
	return filepath.Join(hullHome, "system", "adminer", "hull-login.php")
}

// Runner executes a host command (docker run ...). Injected so scaffold
// logic is testable without an engine; production passes dockerx.Exec.
type Runner func(ctx context.Context, dir string, name string, args ...string) error

// ScaffoldOptions describes a project scaffold request.
type ScaffoldOptions struct {
	// Dir is the project directory (must exist).
	Dir string
	// Version pins the framework version (laravel composer constraint).
	Version string
	// Run executes commands.
	Run Runner
}

// Scaffold populates a fresh project directory for the template , the Go
// port of v1's templates/scripts/*-init.sh.
func Scaffold(ctx context.Context, template string, opts ScaffoldOptions) error {
	switch template {
	case "laravel":
		return scaffoldLaravel(ctx, opts)
	case "plain":
		return scaffoldPlain(opts)
	case "wordpress":
		// WordPress core files are populated by the container on first boot.
		return nil
	case "static":
		return scaffoldStatic(opts)
	case "python":
		return scaffoldPython(opts)
	case "node":
		return scaffoldNode(opts)
	case "go":
		return scaffoldGo(opts)
	default:
		return fmt.Errorf("no scaffolder for template %q", template)
	}
}

// scaffoldNode writes a plain Node project: a stdlib server.js, a package.json,
// and a .gitignore. node_modules lives on a named volume, not in the dir.
func scaffoldNode(opts ScaffoldOptions) error {
	name := filepath.Base(opts.Dir)
	pkg := fmt.Sprintf("{\n  \"name\": %q,\n  \"private\": true,\n  \"scripts\": {\n    \"start\": \"node server.js\"\n  }\n}\n", name)
	return writeAll(opts.Dir, map[string]string{
		"server.js":    nodeServer,
		"package.json": pkg,
		".gitignore":   "node_modules/\nnpm-debug.log*\n",
	})
}

// scaffoldGo writes a plain Go project: a go.mod, a stdlib main.go, an air
// config for rebuild-on-change, and a .gitignore. Written directly, so nothing
// is root-owned; the module and build caches live on named volumes.
func scaffoldGo(opts ScaffoldOptions) error {
	name := filepath.Base(opts.Dir)
	gomod := fmt.Sprintf("module %s\n\ngo %s\n", name, DefaultGo)
	return writeAll(opts.Dir, map[string]string{
		"main.go":    goMain,
		"go.mod":     gomod,
		".air.toml":  airToml,
		".gitignore": "tmp/\n",
	})
}

// writeAll writes each name->content file into dir.
func writeAll(dir string, files map[string]string) error {
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// scaffoldStatic writes a minimal index.html the nginx image serves straight
// from the project directory.
func scaffoldStatic(opts ScaffoldOptions) error {
	return os.WriteFile(filepath.Join(opts.Dir, "index.html"), []byte(staticIndex), 0o644)
}

// scaffoldPython writes a plain Python project: a stdlib app.py, an empty
// requirements.txt, and a .gitignore. Written directly (no container), so there
// are no root-owned files; the venv lives on a named volume, not in the dir.
func scaffoldPython(opts ScaffoldOptions) error {
	files := map[string]string{
		"app.py":           pythonApp,
		"requirements.txt": "# Add your dependencies here, then `hull restart` (or `hull pip install <pkg>`).\n",
		".gitignore":       "__pycache__/\n*.pyc\n.venv/\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(opts.Dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func scaffoldLaravel(ctx context.Context, opts ScaffoldOptions) error {
	if opts.Run == nil {
		return fmt.Errorf("laravel scaffolding requires a command runner")
	}
	target := "laravel/laravel"
	if opts.Version != "" && opts.Version != "latest" {
		target = fmt.Sprintf("laravel/laravel=^%s", opts.Version)
	}

	// Docker's -v parser wants forward slashes even on Windows (a backslash
	// drive path like W:\Sites\app:/app misparses against the : separator).
	mount := filepath.ToSlash(opts.Dir) + ":/app"

	args := []string{"run", "--rm"}
	args = append(args, userFlag()...)
	args = append(args,
		"-v", mount,
		"-w", "/app",
		"composer:latest",
		"sh", "-c", fmt.Sprintf("composer create-project %s tmp && cp -a tmp/. . && rm -rf tmp", target),
	)
	if err := opts.Run(ctx, "", "docker", args...); err != nil {
		return fmt.Errorf("composer create-project failed: %w", err)
	}

	// Hand storage, cache, and database dirs to the container's web user
	// (uid 33), matching v1's laravel-init.sh. `database` is essential:
	// composer create-project (and the initial migrate) run as root, so
	// database/database.sqlite lands root-owned, and Laravel 11+ defaults
	// SESSION_DRIVER=database, so every request writes the session to SQLite.
	// Without a writable database/ (the dir too, for SQLite's journal/WAL),
	// the app 500s on the first request with "attempt to write a readonly
	// database".
	chown := []string{
		"run", "--rm",
		"-v", mount,
		"-w", "/app",
		"alpine", "sh", "-c", "chown -R 33:33 storage bootstrap/cache database",
	}
	if err := opts.Run(ctx, "", "docker", chown...); err != nil {
		return fmt.Errorf("setting storage permissions: %w", err)
	}
	return nil
}

func scaffoldPlain(opts ScaffoldOptions) error {
	return os.WriteFile(filepath.Join(opts.Dir, "index.php"), []byte(plainIndex), 0o644)
}

// userFlag maps the container user to the host user on unix so scaffolded
// files are owned by the developer, not root. No-op on Windows.
func userFlag() []string {
	uid, gid := os.Getuid(), os.Getgid()
	if uid < 0 || gid < 0 {
		return nil
	}
	return []string{"--user", fmt.Sprintf("%d:%d", uid, gid)}
}
