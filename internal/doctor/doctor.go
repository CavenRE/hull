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
		add(Fail, "docker CLI", "not in PATH — install Docker (or a docker-compatible engine)")
	} else {
		dockerFound = true
		add(OK, "docker CLI", "found")
		if v, err := deps.Output(ctx, "", "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
			add(Fail, "container engine", "not responding — is it running?")
		} else {
			add(OK, "container engine", "server "+v)
		}
		if v, err := deps.Output(ctx, "", "docker", "compose", "version", "--short"); err != nil {
			add(Fail, "docker compose", "missing — install the compose plugin")
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

	// System files (self-healing).
	xdebug := filepath.Join(cfg.HullHome, "system", "php", "xdebug.ini")
	if _, err := os.Stat(xdebug); err != nil {
		if err := templates.EnsureSystemFiles(cfg.HullHome); err == nil {
			add(OK, "system files", "were missing — provisioned now")
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
				add(Warn, "caddy network", "missing — created automatically on first start")
			}
		}
	}
	if cfg.Router.Enabled {
		if portListening(cfg.Router.HTTPSPort) {
			add(OK, "router (embedded)", fmt.Sprintf("listening on :%d", cfg.Router.HTTPSPort))
		} else {
			add(Warn, "router (embedded)", fmt.Sprintf("enabled but :%d not listening — start the daemon (hulld)", cfg.Router.HTTPSPort))
		}
		if certs.Trusted(cfg.RouterDataDir()) {
			add(OK, "certificate", "local CA provisioned (trust install: hull trust)")
		} else {
			add(Warn, "certificate", "no local CA yet — run: hull trust")
		}
	} else if dockerFound {
		if out, err := deps.Output(ctx, "", "docker", "ps", "--format", "{{.Names}}"); err == nil {
			if containsLine(out, "hull-router") {
				add(OK, "router", "hull-router running (v1 stack)")
			} else {
				add(Warn, "router", "no router active — run `hull setup` for v2-native routing")
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
		add(Warn, "name resolution", "no *."+cfg.TLD+" resolution yet — run `hull setup`")
	}

	// Daemon.
	if deps.DaemonVersion != "" {
		add(OK, "daemon", "running ("+deps.DaemonVersion+")")
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

func portListening(port int) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func containsLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
