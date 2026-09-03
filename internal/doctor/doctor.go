// Package doctor diagnoses a Hull environment. The CLI renders its checks
// in the terminal; the daemon serves them at GET /v1/doctor for the GUI's
// onboarding wizard and Settings panel.
package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CavenRE/hull/internal/certs"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// Status of one check.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Check is one diagnostic result.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Deps injects the environment probes (real in production, stubs in tests).
type Deps struct {
	LookPath func(file string) (string, error)
	Output   func(ctx context.Context, dir, name string, args ...string) (string, error)
	// DaemonVersion is non-empty when a daemon is known to be running
	// (the API server passes its own version; the CLI probes first).
	DaemonVersion string
}

// Run executes all checks.
func Run(ctx context.Context, cfg *config.Config, deps Deps) []Check {
	var checks []Check
	add := func(status Status, name, detail string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail})
	}

	// Container engine.
	dockerFound := false
	if _, err := deps.LookPath("docker"); err != nil {
		add(Fail, "docker CLI", "not in PATH , install Docker (or a docker-compatible engine)")
	} else {
		dockerFound = true
		add(OK, "docker CLI", "found")
		if v, err := deps.Output(ctx, "", "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
			if isDockerPermissionErr(err) {
				add(Fail, "container engine", "permission denied on the docker socket , add yourself to the 'docker' group: sudo usermod -aG docker $USER  (then log out and back in)")
			} else {
				add(Fail, "container engine", "not responding , is it running?")
			}
		} else {
			add(OK, "container engine", "server "+v)
		}
		if v, err := deps.Output(ctx, "", "docker", "compose", "version", "--short"); err != nil {
			add(Fail, "docker compose", "missing , install the compose plugin")
		} else {
			add(OK, "docker compose", v)
		}
	}

	// Config + roots.
	add(OK, "config", fmt.Sprintf("tld=%s home=%s", cfg.TLD, cfg.HullHome))
	for _, root := range cfg.Roots {
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			add(Warn, "root "+root, "does not exist yet")
		} else {
			add(OK, "root "+root, "ok")
		}
	}

	// Windows / WSL bind-mount performance. Docker serves files from the Windows
	// filesystem to Linux containers over a slow 9p mount, so PHP page loads run
	// multiple seconds and a cold start can 502 until the app warms. Warn when a
	// root is on a Windows drive (a drive letter on Windows, or /mnt/<drive> when
	// Hull runs inside WSL) and point at the real fix plus a stopgap.
	for _, root := range cfg.Roots {
		if onWindowsFilesystem(root) {
			opcache := filepath.Join(cfg.HullHome, "system", "php", "opcache.ini")
			add(Warn, "performance", "root "+root+" is on the Windows filesystem, which Docker serves to containers over a slow 9p mount (multi-second PHP page loads; a 502 on a cold start until the app warms). The real fix is to keep sites in the WSL2 Linux filesystem (ext4): run Hull inside WSL with projects under your Linux home. Stopgap: set opcache.validate_timestamps=0 in "+opcache+" to stop the per-request re-stat spikes (then restart a container after editing PHP). Also exclude the sites folder and Docker's data VHDX from Windows Defender.")
			break
		}
	}

	// Duplicate project names. Scan resolves a name to one directory and silently
	// skips the rest, so a stray copy of a hull.yaml (a backup folder, a clone)
	// can make Hull operate on the wrong directory with no warning anywhere.
	for _, c := range state.Collisions(cfg.Roots, cfg.Projects...) {
		add(Warn, "duplicate name "+c.Name, "resolves to more than one directory; Hull uses one and silently ignores the rest: "+strings.Join(c.Dirs, ", "))
	}

	// System files (self-healing).
	xdebug := filepath.Join(cfg.HullHome, "system", "php", "xdebug.ini")
	if _, err := os.Stat(xdebug); err != nil {
		if err := templates.EnsureSystemFiles(cfg.HullHome); err == nil {
			add(OK, "system files", "were missing , provisioned now")
		} else {
			add(Warn, "system files", "missing and could not be written: "+err.Error())
		}
	} else {
		add(OK, "system files", "present")
	}

	// Routing.
	if dockerFound {
		if out, err := deps.Output(ctx, "", "docker", "network", "ls", "--format", "{{.Name}}"); err == nil {
			if containsLine(out, "caddy") {
				add(OK, "caddy network", "exists")
			} else {
				add(Warn, "caddy network", "missing , created automatically on first start")
			}
		}
	}
	if cfg.Router.Enabled {
		if portListening(cfg.Router.Loopback, cfg.Router.HTTPSPort) {
			add(OK, "router (embedded)", fmt.Sprintf("listening on %s:%d", cfg.Router.Loopback, cfg.Router.HTTPSPort))
		} else if deps.DaemonVersion != "" {
			// Daemon is up but the port isn't bound , not a "start the daemon"
			// problem. On stock Linux this is the silent CAP_NET_BIND_SERVICE
			// failure; otherwise a port conflict.
			add(Fail, "router (embedded)", fmt.Sprintf("daemon is running but %s:%d is not bound , likely missing CAP_NET_BIND_SERVICE (run `hull setup`, or `sudo setcap cap_net_bind_service=+ep <hull>`) or a port conflict", cfg.Router.Loopback, cfg.Router.HTTPSPort))
		} else {
			add(Warn, "router (embedded)", fmt.Sprintf("enabled but %s:%d not listening , start the daemon (hulld)", cfg.Router.Loopback, cfg.Router.HTTPSPort))
		}
		if certs.Trusted(cfg.RouterDataDir()) {
			add(OK, "certificate", "local CA present (file only; not proof routing is live , trust install: hull trust)")
		} else {
			add(Warn, "certificate", "no local CA yet , run: hull trust")
		}
	} else if dockerFound {
		if out, err := deps.Output(ctx, "", "docker", "ps", "--format", "{{.Names}}"); err == nil {
			if containsLine(out, "hull-router") {
				add(OK, "router", "hull-router running (v1 stack)")
			} else {
				add(Warn, "router", "no router active , run `hull setup` for v2-native routing")
			}
		}
	}

	// Name resolution. Two valid mechanisms: a real wildcard resolver
	// (*.tld → 127.0.0.1, used on Linux/macOS), or the managed hosts-file
	// block (Windows, where browsers bypass system DNS). Either is healthy.
	probe := "hull-doctor-probe." + cfg.TLD
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if addrs, err := net.DefaultResolver.LookupHost(lookupCtx, probe); err == nil && len(addrs) > 0 {
		add(OK, "name resolution", fmt.Sprintf("wildcard *.%s → %s", cfg.TLD, addrs[0]))
	} else if n := platform.ManagedHostsCount(); n > 0 {
		add(OK, "name resolution", fmt.Sprintf("%d site name(s) via the managed hosts block", n))
	} else {
		add(Warn, "name resolution", "no *."+cfg.TLD+" resolution yet , run `hull setup`")
	}

	// Daemon. When the embedded router/DNS are enabled, a stopped daemon means
	// routing and name resolution are actually offline , report that honestly
	// (Warn) instead of green. The CLI itself still works in-process.
	if deps.DaemonVersion != "" {
		add(OK, "daemon", "running ("+deps.DaemonVersion+")")
	} else if cfg.Router.Enabled || cfg.DNS.Enabled {
		add(Warn, "daemon", "not running , routing/DNS are offline until you start it (hulld); the CLI still works in-process")
	} else {
		add(OK, "daemon", "not running (CLI operates in-process)")
	}

	return checks
}

// Fatal reports whether any check is a hard failure.
func Fatal(checks []Check) bool {
	for _, c := range checks {
		if c.Status == Fail {
			return true
		}
	}
	return false
}

func portListening(host string, port int) bool {
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", host+":"+strconv.Itoa(port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// isDockerPermissionErr reports whether a failed docker probe was a socket
// permission denial (user not in the docker group) rather than the engine
// being down , the two need very different fixes.
func isDockerPermissionErr(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "permission denied") &&
		(strings.Contains(s, "docker.sock") || strings.Contains(s, "/var/run/docker") || strings.Contains(s, "dial unix"))
}

// onWindowsFilesystem reports whether a project root lives on the Windows
// filesystem, which Docker shares to containers over a slow 9p mount: a
// drive-letter path on native Windows (C:\...), or a /mnt/<drive> path when Hull
// runs inside a WSL distro.
func onWindowsFilesystem(root string) bool {
	if vol := filepath.VolumeName(root); len(vol) == 2 && vol[1] == ':' {
		return true // C:\ on native Windows
	}
	r := filepath.ToSlash(root)
	if len(r) >= 7 && strings.HasPrefix(r, "/mnt/") && r[6] == '/' {
		c := r[5]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}

func containsLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
