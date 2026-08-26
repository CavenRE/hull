package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/config"
)

func TestOnWindowsFilesystem(t *testing.T) {
	// /mnt/<drive> is detected on every OS (a plain string check), so the
	// warning fires when Hull runs inside WSL against a project on a Windows drive.
	for _, r := range []string{"/mnt/c/Sites/app", "/mnt/d/x"} {
		if !onWindowsFilesystem(r) {
			t.Errorf("onWindowsFilesystem(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"/home/me/app", "/mnt/wsl/x", "/mnt/", "/srv/c", "/mntfoo/c/x"} {
		if onWindowsFilesystem(r) {
			t.Errorf("onWindowsFilesystem(%q) = true, want false", r)
		}
	}
	// Drive letters are only recognized by the OS-specific VolumeName on Windows.
	if runtime.GOOS == "windows" && !onWindowsFilesystem(`C:\Sites\app`) {
		t.Error(`onWindowsFilesystem("C:\\Sites\\app") = false on windows, want true`)
	}
}

func findCheck(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestRunDockerMissingIsFatal(t *testing.T) {
	cfg := &config.Config{TLD: "test", HullHome: t.TempDir(), Roots: []string{t.TempDir()}}
	deps := Deps{
		LookPath: func(string) (string, error) { return "", errors.New("not in PATH") },
		Output:   func(context.Context, string, string, ...string) (string, error) { return "", errors.New("unused") },
	}
	checks := Run(context.Background(), cfg, deps)
	c, ok := findCheck(checks, "docker CLI")
	if !ok || c.Status != Fail {
		t.Fatalf("docker CLI check = %+v (ok=%v), want Fail", c, ok)
	}
	if !Fatal(checks) {
		t.Error("Fatal should be true when the docker CLI is missing")
	}
}

func TestRunHealthy(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{TLD: "test", HullHome: home, Roots: []string{t.TempDir()}}
	deps := Deps{
		LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		Output: func(_ context.Context, _, _ string, args ...string) (string, error) {
			switch {
			case len(args) > 0 && args[0] == "compose":
				return "v2.29.0", nil
			case len(args) > 0 && args[0] == "version":
				return "27.0.3", nil
			case len(args) > 0 && args[0] == "network":
				return "bridge\ncaddy\nhost", nil
			default: // ps, etc.
				return "", nil
			}
		},
		DaemonVersion: "1.2.3",
	}
	checks := Run(context.Background(), cfg, deps)
	if Fatal(checks) {
		t.Fatalf("a healthy environment should not be Fatal: %+v", checks)
	}
	if c, _ := findCheck(checks, "container engine"); c.Status != OK || !strings.Contains(c.Detail, "27.0.3") {
		t.Errorf("container engine = %+v", c)
	}
	if c, _ := findCheck(checks, "docker compose"); c.Status != OK {
		t.Errorf("docker compose = %+v", c)
	}
	if c, _ := findCheck(checks, "caddy network"); c.Status != OK {
		t.Errorf("caddy network = %+v", c)
	}
	if c, _ := findCheck(checks, "daemon"); c.Status != OK || !strings.Contains(c.Detail, "1.2.3") {
		t.Errorf("daemon = %+v", c)
	}
	// Run self-heals missing system files.
	if _, err := os.Stat(filepath.Join(home, "system", "php", "xdebug.ini")); err != nil {
		t.Errorf("system files not provisioned by Run: %v", err)
	}
}

func TestRunDaemonDownWarnsWhenRouterEnabled(t *testing.T) {
	// With the embedded router/DNS enabled, a stopped daemon means routing is
	// actually offline , the daemon check must Warn (not read green), so health
	// stops lying after `hull stop`.
	cfg := &config.Config{TLD: "test", HullHome: t.TempDir(), Roots: []string{t.TempDir()}}
	cfg.Router.Enabled = true
	deps := Deps{
		LookPath:      func(string) (string, error) { return "", errors.New("no docker") },
		Output:        func(context.Context, string, string, ...string) (string, error) { return "", nil },
		DaemonVersion: "", // daemon is down
	}
	checks := Run(context.Background(), cfg, deps)
	c, ok := findCheck(checks, "daemon")
	if !ok || c.Status != Warn {
		t.Errorf("daemon check = %+v (ok=%v), want Warn when router enabled and daemon down", c, ok)
	}
}

func TestRunDaemonDownOKWhenRouterDisabled(t *testing.T) {
	// Without the embedded router, the CLI genuinely works in-process , a down
	// daemon is fine (OK), not a warning.
	cfg := &config.Config{TLD: "test", HullHome: t.TempDir(), Roots: []string{t.TempDir()}}
	deps := Deps{
		LookPath: func(string) (string, error) { return "", errors.New("no docker") },
		Output:   func(context.Context, string, string, ...string) (string, error) { return "", nil },
	}
	checks := Run(context.Background(), cfg, deps)
	c, ok := findCheck(checks, "daemon")
	if !ok || c.Status != OK {
		t.Errorf("daemon check = %+v (ok=%v), want OK when router disabled", c, ok)
	}
}

func TestRunMissingRootWarns(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-created")
	cfg := &config.Config{TLD: "test", HullHome: t.TempDir(), Roots: []string{missing}}
	deps := Deps{
		LookPath: func(string) (string, error) { return "", errors.New("no docker") },
		Output:   func(context.Context, string, string, ...string) (string, error) { return "", nil },
	}
	checks := Run(context.Background(), cfg, deps)
	c, ok := findCheck(checks, "root "+missing)
	if !ok || c.Status != Warn {
		t.Errorf("missing-root check = %+v (ok=%v), want Warn", c, ok)
	}
}
