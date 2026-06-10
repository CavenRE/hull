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

// EnsureSystemFiles writes Hull-owned support files (the shared xdebug.ini
// every PHP container mounts) into the Hull home directory if missing, so a
// fresh v2 machine works without a v1 installation. Existing files are left
// untouched — they may carry user tweaks.
func EnsureSystemFiles(hullHome string) error {
	target := filepath.Join(hullHome, "system", "php", "xdebug.ini")
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(xdebugINI), 0o644)
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

// Scaffold populates a fresh project directory for the template — the Go
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
	default:
		return fmt.Errorf("no scaffolder for template %q", template)
	}
}

func scaffoldLaravel(ctx context.Context, opts ScaffoldOptions) error {
	if opts.Run == nil {
		return fmt.Errorf("laravel scaffolding requires a command runner")
	}
	target := "laravel/laravel"
	if opts.Version != "" && opts.Version != "latest" {
		target = fmt.Sprintf("laravel/laravel=^%s", opts.Version)
	}

	args := []string{"run", "--rm"}
	args = append(args, userFlag()...)
	args = append(args,
		"-v", opts.Dir+":/app",
		"-w", "/app",
		"composer:latest",
		"sh", "-c", fmt.Sprintf("composer create-project %s tmp && cp -a tmp/. . && rm -rf tmp", target),
	)
	if err := opts.Run(ctx, "", "docker", args...); err != nil {
		return fmt.Errorf("composer create-project failed: %w", err)
	}

	// Hand storage and cache dirs to the container's web user (uid 33),
	// matching v1's laravel-init.sh.
	chown := []string{
		"run", "--rm",
		"-v", opts.Dir + ":/app",
		"-w", "/app",
		"alpine", "sh", "-c", "chown -R 33:33 storage bootstrap/cache",
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
