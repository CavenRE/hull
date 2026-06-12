package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dns"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/router"
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
		server := &dns.Server{TLD: cfg.TLD, Addr: "127.0.0.1:" + strconv.Itoa(cfg.DNS.Port)}
		if err := server.Start(); err != nil {
			stop()
			return nil, nil, err
		}
		logf("dns: answering *.%s on %s", cfg.TLD, server.LocalAddr())
		stops = append(stops, server.Stop)
	}

	if cfg.Router.Enabled {
		opts := router.Options{
			HTTPPort:  cfg.Router.HTTPPort,
			HTTPSPort: cfg.Router.HTTPSPort,
			DataDir:   cfg.RouterDataDir(),
		}
		lastFingerprint := "\x00never-applied" // force the first Apply even with zero routes
		sync := func() {
			routes := eng.Routes(ctx)
			fp := fingerprint(routes)
			if fp == lastFingerprint {
				return
			}
			if err := router.Apply(routes, opts); err != nil {
				logf("router: apply failed: %v", err)
				return
			}
			lastFingerprint = fp
			logf("router: %d route(s) active", len(routes))
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
		logf("router: https on :%d (data %s)", opts.HTTPSPort, opts.DataDir)
	}

	return stop, syncNow, nil
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
