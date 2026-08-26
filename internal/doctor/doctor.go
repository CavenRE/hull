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
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/CavenRE/hull/internal/certs"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/platform"
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

	// Windows performance. Docker Desktop runs Linux in a WSL2 VM, so bind
	// mounting project files that live on the Windows filesystem makes every
	// PHP request read across the VM boundary , the usual cause of slow page
	// loads. Warn when a root sits on a Windows drive and point at the fix.
	if runtime.GOOS == "windows" {
		for _, root := range cfg.Roots {
			if vol := filepath.VolumeName(root); len(vol) == 2 && vol[1] == ':' {
				add(Warn, "performance (Windows)", "root "+root+" is on the Windows filesystem; Docker bind-mount I/O there makes PHP pages slow to load. Keep sites in the WSL2 Linux filesystem (run Hull inside WSL, or store them under \\\\wsl$\\...), and exclude the sites folder and Docker's data VHDX from Windows Defender real-time scanning.")
			}
		}
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

func containsLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
