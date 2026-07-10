package api

import (
	"context"
	"errors"
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

// RouteSyncInterval is how often the daemon reconciles the route table.
// Variable so tests can shrink it.
var RouteSyncInterval = 3 * time.Second

// startNetworking boots the embedded router and DNS server per config and
// returns a stop function. A nil error with networking disabled is normal.
func startNetworking(ctx context.Context, cfg *config.Config, eng *engine.Engine, logf func(string, ...any)) (stop func(), syncNow func(), err error) {
	stops := []func(){}
	stop = func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
	}
	syncNow = func() {}

	if cfg.DNS.Enabled {
		server := &dns.Server{TLD: cfg.TLD, Addr: "127.0.0.1:" + strconv.Itoa(cfg.DNS.Port), Answer: net.ParseIP(cfg.Router.Loopback)}
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
		lastHosts := "\x00never-synced"
		var syncMu sync.Mutex
		sync := func() {
			// Serialize reconciles: the ticker, initial call, and many
			// handlers (several via `go SyncRoutes()`) plus rebuild/reset jobs
			// can overlap, and SyncHosts shells out to edit the hosts file.
			syncMu.Lock()
			defer syncMu.Unlock()
			svcRoutes, svcDomains := serviceUI(ctx, cfg)
			live := append(eng.Routes(ctx), svcRoutes...)

			// Every known domain (running or not) for both the hosts block and
			// router vhosts , a stopped site keeps a vhost so it answers a
			// readable 502 instead of an opaque TLS handshake failure.
			var allDomains []string
			if projects, err := state.Scan(cfg.Roots); err == nil {
				allDomains = append(engine.AllDomains(projects, cfg.TLD), svcDomains...)
			} else {
				allDomains = svcDomains
			}

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
			// is the layer that makes names resolve. No-op (and no UAC)
			// when unchanged.
			{
				domains := append([]string(nil), allDomains...)
				sort.Strings(domains)
				hostsKey := strings.Join(domains, ";")
				if hostsKey != lastHosts {
					if err := platform.SyncHosts(domains, cfg.Router.Loopback); err != nil {
						logf("hosts: %v", err)
					} else {
						lastHosts = hostsKey
						logf("hosts: block synced (%d domain(s))", len(domains))
					}
				}
			}
		}
		sync() // initial table (also boots the router with zero routes)
		syncNow = sync

		ticker := time.NewTicker(RouteSyncInterval)
		done := make(chan struct{})
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
