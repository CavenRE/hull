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
	// Fix describes what `hull doctor --fix` would do about this check, and is
	// empty when Hull cannot fix it automatically. Naming the remedy on the
	// check is what stops the flag being a black box.
	Fix string `json:"fix,omitempty"`
	// Commands are ready-to-run commands for something Hull will not do itself,
	// such as an antivirus exclusion that needs an elevated shell and is the
	// user's decision to make.
	Commands []string `json:"commands,omitempty"`
}

// Deps injects the environment probes (real in production, stubs in tests).
type Deps struct {
	LookPath func(file string) (string, error)
	Output   func(ctx context.Context, dir, name string, args ...string) (string, error)
	// DaemonVersion is non-empty when a daemon is known to be running
	// (the API server passes its own version; the CLI probes first).
	DaemonVersion string
	// Measure opts into the checks that cost real time and do real work: the
	// container benchmark (which starts a container and writes a temporary
	// directory into a project root) and the WSL queries (which can boot a
	// distribution). Off by default, and deliberately so: the daemon serves
	// GET /v1/doctor with no deadline, so a GUI opening its Settings panel must
	// not be made to wait a minute and a half for a benchmark it did not ask
	// for. The CLI turns it on, where the user typed the command and is waiting.
	Measure bool
}

// Run executes all checks.
func Run(ctx context.Context, cfg *config.Config, deps Deps) []Check {
	var checks []Check
	add := func(status Status, name, detail string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail})
	}
	// addFix is add for a check Hull can act on, so the existing twenty-odd call
	// sites stay as they are.
	addFix := func(status Status, name, detail, fix string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail, Fix: fix})
	}

	// Container engine.
	dockerFound, engineUp := false, false
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
			engineUp = true
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
			addFix(Warn, "root "+root, "does not exist yet", "create it")
		} else {
			add(OK, "root "+root, "ok")
		}
	}

	// Windows / WSL bind-mount performance. Docker serves files from the Windows
	// filesystem to Linux containers over a slow 9p mount, so PHP page loads run
	// multiple seconds and a cold start can 502 until the app warms.
	var slowRoots []string
	for _, root := range cfg.Roots {
		if platform.OnWindowsFilesystem(root) {
			slowRoots = append(slowRoots, root)
		}
	}
	if len(slowRoots) > 0 {
		msg := "root " + strings.Join(slowRoots, ", ") + " is on the Windows filesystem, which Docker serves to containers over a slow 9p mount (multi-second PHP page loads; a 502 on a cold start until the app warms). "
		if len(slowRoots) > 1 {
			msg = "roots " + strings.Join(slowRoots, ", ") + " are on the Windows filesystem, which Docker serves to containers over a slow 9p mount (multi-second PHP page loads; a 502 on a cold start until the app warms). "
		}
		fix := ""
		if cfg.AutoReloadEnabled() {
			msg += "Hull already takes the biggest cost off it: PHP no longer re-checks every cached file on each request, and the daemon watches your sources and clears the opcode cache when you save. "
		} else {
			msg += "auto_reload is off in config.yaml, so PHP re-checks every cached file on every request, which roughly doubles a page load here. "
			fix = "turn auto_reload back on"
		}
		msg += "The real fix is to move projects into the WSL2 Linux filesystem, which is not merely faster but as fast as the container's own disk: `hull move <project> --to-wsl`."
		addFix(Warn, "performance", msg, fix)

		// Measure it rather than asserting it. A number the user can compare
		// against a native filesystem is far harder to argue with than prose,
		// and it is the number that explains where the seconds go.
		// Skip a root that is not there yet. Docker creates a missing bind
		// source rather than refusing, so probing one would quietly conjure the
		// very directory the check above just reported as missing, and the two
		// lines would contradict each other.
		probeRoot := ""
		for _, root := range slowRoots {
			if info, err := os.Stat(root); err == nil && info.IsDir() {
				probeRoot = root
				break
			}
		}
		// Gated on the engine ANSWERING, not merely on the docker binary being
		// installed: running a container against a closed engine only produces a
		// failure this check already reported one line above.
		if deps.Measure && engineUp && probeRoot != "" {
			speed, err := MountProbe(ctx, deps, probeRoot)
			switch {
			case err != nil:
				// Never Fail: doctor's exit code is a health gate, and a
				// machine with the engine closed is not unhealthy.
				add(Warn, "mount speed", "could not measure "+probeRoot+" ("+err.Error()+")")
			case !speed.Writable:
				add(Warn, "mount speed", probeRoot+" is not writable from a container, so it could not be measured")
			default:
				detail := fmt.Sprintf("%s costs %.2f ms per file operation, against %s ms on a filesystem the same container owns (%.0fx). A page that opens 2,000 files spends about %.1f s of its load in the filesystem alone.",
					probeRoot, speed.MsPerOp, speed.NativeText(), speed.Ratio(), speed.MsPerOp*2000/1000)
				if speed.MsPerOp >= slowMsPerOp {
					add(Warn, "mount speed", detail)
				} else {
					add(OK, "mount speed", detail)
				}
			}
		}

		// Antivirus. Real-time scanning of every file read compounds the mount
		// cost and is the usual explanation for a page that normally takes 7
		// seconds occasionally taking 16. Hull hands over the commands rather
		// than touching anyone's security settings.
		if cmds := platform.DefenderCommands(cfg.Roots); len(cmds) > 0 {
			checks = append(checks, Check{
				Name:     "antivirus",
				Status:   Warn,
				Detail:   "if you use Microsoft Defender, excluding your sites and Docker's disk images stops it scanning every file a container reads. Run these in an elevated PowerShell (Hull will not change your security settings for you):",
				Commands: cmds,
			})
		}

		// The paved road out. Only worth mentioning when the machine can
		// actually take it.
		if deps.Measure && platform.WSLAvailable() {
			if distros := platform.WSLDistros(ctx); len(distros) > 0 {
				d := distros[0]
				if platform.WSLDockerIntegration(ctx, d) {
					add(OK, "wsl", "distro "+d+" is available with Docker integration on, so `hull move <project> --to-wsl` will make that project as fast as a native Linux setup")
				} else {
					add(Warn, "wsl", "distro "+d+" is available but Docker Desktop's WSL integration is off for it, which is what a fast project mount needs. Turn it on in Docker Desktop: Settings > Resources > WSL Integration > "+d+", then Apply & Restart. After that, `hull move <project> --to-wsl`")
				}
			}
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

func containsLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
