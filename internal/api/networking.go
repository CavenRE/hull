package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dns"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/router"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// RouteSyncInterval is how often the daemon reconciles the route table with
// no trigger. Container changes arrive as engine events and API changes call
// SyncRoutes, so the tick only catches edits made on disk. Variable so tests
// can shrink it.
var RouteSyncInterval = 15 * time.Second

// engineEventDebounce folds a burst of container events (a compose up starts
// several containers) into one reconcile; engineEventRetry is how long a
// broken event stream waits before it reconnects.
var (
	engineEventDebounce = 300 * time.Millisecond
	engineEventRetry    = 5 * time.Second
)

// watchEngine follows the engine's container events through events and calls
// onChange once per burst. What changes while the stream is down, the route
// tick catches. stop ends the stream and waits for both goroutines, so no
// `docker events` process outlives the daemon.
func watchEngine(ctx context.Context, events func(context.Context, func()) error, onChange func(), logf func(string, ...any)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	kick := make(chan struct{}, 1)
	signal := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-kick:
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(engineEventDebounce):
			}
			select {
			case <-kick: // arrived during the wait; this reconcile covers it
			default:
			}
			onChange()
		}
	}()
	go func() {
		defer wg.Done()
		lastErr := ""
		for {
			err := events(ctx, signal)
			if ctx.Err() != nil {
				return
			}
			if msg := fmt.Sprint(err); err != nil && !errors.Is(err, dockerx.ErrEngineDown) && msg != lastErr {
				logf("docker: event stream: %v (retrying every %s)", err, engineEventRetry)
				lastErr = msg
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(engineEventRetry):
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}

// startNetworking boots the embedded router and DNS server per config and
// returns a stop function. A nil error with networking disabled is normal.
func startNetworking(ctx context.Context, cfgOf func() *config.Config, eng *engine.Engine, logf func(string, ...any)) (stop func(), syncNow func(), err error) {
	cfg := cfgOf() // bind addresses and ports: read once, a change needs a restart
	stops := []func(){}
	stop = func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
	}
	syncNow = func() {}

	if cfg.DNS.Enabled {
		server := &dns.Server{TLD: cfg.TLD, Addr: cfg.Router.Loopback + ":" + strconv.Itoa(cfg.DNS.Port), Answer: net.ParseIP(cfg.Router.Loopback)}
		if err := server.Start(); err != nil {
			// Degrade, never die: routing is the critical path, and hosts
			// file entries keep resolving without us.
			logf("dns: DISABLED , %v (names in the hosts file still resolve; router unaffected)", err)
		} else {
			if server.TCPErr != nil {
				logf("dns: udp-only , %v (fine for resolver lookups)", server.TCPErr)
			}
			logf("dns: answering *.%s on %s", cfg.TLD, server.LocalAddr())
			stops = append(stops, server.Stop)
		}
	}

	if cfg.Router.Enabled {
		opts := router.Options{
			HTTPPort:  cfg.Router.HTTPPort,
			HTTPSPort: cfg.Router.HTTPSPort,
			DataDir:   cfg.RouterDataDir(),
			BindHost:  cfg.Router.Loopback,
		}
		lastFingerprint := "\x00never-applied" // force the first Apply even with zero routes
		lastScanErr := ""
		hostsWants := make(chan hostsWant, 1)
		done := make(chan struct{})
		go syncHostsLoop(ctx, done, hostsWants, cfg.Router.Loopback, logf)
		// Tracks engine reachability so the loop logs transitions, not every tick.
		lastEngineUp := true
		var syncMu sync.Mutex
		sync := func() {
			// Serialize reconciles: the ticker, engine events, the initial
			// call, and many handlers (several via `go SyncRoutes()`) plus rebuild/reset jobs
			// can overlap, and SyncHosts shells out to edit the hosts file.
			syncMu.Lock()
			defer syncMu.Unlock()

			// With the engine down every container lookup below fails, and the
			// loop used to discard those errors twice per tick: dozens of futile
			// docker spawns a minute, no log line, and every site silently
			// dropping to a 502. Probe once, log only the transition (so the log
			// records when Docker went away and came back rather than repeating
			// itself on every sync), and skip the container half of the sync.
			// The hosts block and vhosts below come from disk, so they keep
			// working and a stopped site still answers a readable 502.
			engineUp := dockerx.EngineCheck(ctx) == nil
			if engineUp != lastEngineUp {
				if engineUp {
					logf("docker: engine is back")
				} else {
					logf("docker: engine is not running; container state is unavailable until it returns")
				}
				lastEngineUp = engineUp
			}

			cur := cfgOf()
			var svcRoutes []router.Route
			var svcDomains []string
			var live []router.Route
			if engineUp {
				svcRoutes, svcDomains = serviceUI(ctx, cur)
				live = append(eng.Routes(ctx), svcRoutes...)
			}

			// Every known domain (running or not) for both the hosts block and
			// router vhosts , a stopped site keeps a vhost so it answers a
			// readable 502 instead of an opaque TLS handshake failure.
			// A root that cannot be read is logged when it first fails; the other
			// roots' sites keep their routes and hosts entries.
			projects, scanErr := state.Scan(cur.Roots, cur.Projects...)
			if msg := fmt.Sprint(scanErr); scanErr != nil && msg != lastScanErr {
				logf("projects: %v", scanErr)
				lastScanErr = msg
			} else if scanErr == nil {
				lastScanErr = ""
			}
			allDomains := append(engine.AllDomains(eng.Routable(projects), cur.TLD), svcDomains...)

			opts.TLD = cur.TLD
			routes := mergeDownRoutes(live, allDomains)
			fp := fingerprint(routes)
			if fp != lastFingerprint {
				if err := router.Apply(routes, opts); err != nil {
					if isBindPermissionErr(err) {
						logf("router: CANNOT BIND %s:%d/%d , permission denied. The embedded router needs CAP_NET_BIND_SERVICE; run `hull setup` (grants it) or `sudo setcap cap_net_bind_service=+ep <hull binary>`. https://<name>.%s stays offline until then.", opts.BindHost, opts.HTTPPort, opts.HTTPSPort, cfg.TLD)
					} else {
						logf("router: apply failed: %v", err)
					}
				} else {
					lastFingerprint = fp
					logf("router: %d route(s) active (%d live)", len(routes), len(live))
				}
			}

			// Hosts block covers ALL managed sites and service UIs
			// (running or not): browsers bypass NRPT on Windows, so this
			// is the layer that makes names resolve. Handed to its own
			// goroutine, keeping only the newest list.
			domains := append([]string(nil), allDomains...)
			sort.Strings(domains)
			select {
			case <-hostsWants:
			default:
			}
			hostsWants <- hostsWant{domains: domains, tld: cur.TLD}
		}
		sync() // initial table (also boots the router with zero routes)
		syncNow = sync

		ticker := time.NewTicker(RouteSyncInterval)
		go func() {
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					sync()
				}
			}
		}()
		stops = append(stops, func() {
			ticker.Stop()
			close(done)
			_ = router.Stop()
		})
		bindHost := cfg.Router.Loopback
		if bindHost == "" {
			bindHost = "127.0.0.1"
		}
		logf("router: https on %s:%d (data %s)", bindHost, opts.HTTPSPort, opts.DataDir)
	}

	return stop, syncNow, nil
}

// hostsRetry is how long a failed hosts write (on Windows, usually a declined
// UAC prompt) waits before Hull asks again for the same names.
var hostsRetry = 10 * time.Minute

type hostsWant struct {
	domains []string
	tld     string
}

// syncHostsLoop applies the hosts block off the route-sync path: on Windows a
// write waits on a UAC prompt, which must never stall route sync or daemon
// start. No-op (and no UAC) when unchanged, and a failed write is retried for
// the same names only after hostsRetry, so a declined prompt does not come
// back every few seconds.
func syncHostsLoop(ctx context.Context, done <-chan struct{}, wants <-chan hostsWant, ip string, logf func(string, ...any)) {
	last := "\x00never-synced"
	failed, failedAt := "", time.Time{}
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case w := <-wants:
			key := w.tld + "|" + strings.Join(w.domains, ";")
			if key == last || (key == failed && time.Since(failedAt) < hostsRetry) {
				continue
			}
			if err := platform.SyncHosts(w.domains, ip, w.tld); err != nil {
				failed, failedAt = key, time.Now()
				logf("hosts: %v (asking again in %s, or sooner if the site list changes)", err, hostsRetry)
				continue
			}
			last, failed = key, ""
			logf("hosts: block synced (%d domain(s))", len(w.domains))
		}
	}
}

// serviceUI lists routes and hostnames for shared instances with embedded
// web UIs (mailpit at mail.<tld>; adminer later).
func serviceUI(ctx context.Context, cfg *config.Config) (routes []router.Route, domains []string) {
	instances, err := services.NewManager(cfg).List(ctx)
	if err != nil {
		return nil, nil
	}
	for _, in := range instances {
		def, ok := templates.Engine(in.Engine)
		if !ok || def.UIPort == 0 || def.UISubdomain == "" {
			continue
		}
		domain := def.UISubdomain + "." + cfg.TLD
		domains = append(domains, domain)
		if !in.Running {
			continue
		}
		if port, err := dockerx.PublishedPort(ctx, in.Dir, def.Name, def.UIPort); err == nil {
			routes = append(routes, router.Route{Domain: domain, Upstream: "127.0.0.1:" + strconv.Itoa(port)})
		}
	}
	return routes, domains
}

// mergeDownRoutes adds a placeholder route (empty upstream , rendered as a 502
// by the router) for every known domain that has no live upstream, so a
// stopped or crashed site returns a readable page instead of failing the TLS
// handshake.
func mergeDownRoutes(live []router.Route, allDomains []string) []router.Route {
	have := make(map[string]bool, len(live))
	for _, r := range live {
		have[r.Domain] = true
	}
	routes := append([]router.Route(nil), live...)
	for _, d := range allDomains {
		if d == "" || have[d] {
			continue
		}
		have[d] = true
		routes = append(routes, router.Route{Domain: d}) // empty upstream → 502
	}
	return routes
}

// isBindPermissionErr reports whether err is a privileged-port bind denial.
// Caddy wraps the underlying listen error as text, so we check both the errno
// and the message.
func isBindPermissionErr(err error) bool {
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "permission denied") && (strings.Contains(s, "bind") || strings.Contains(s, "listen"))
}

func fingerprint(routes []router.Route) string {
	var sb strings.Builder
	for _, r := range routes {
		sb.WriteString(r.Domain)
		sb.WriteByte('=')
		sb.WriteString(r.Upstream)
		sb.WriteByte(';')
	}
	return sb.String()
}
