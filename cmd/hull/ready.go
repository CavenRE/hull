package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

// upReadyTimeout bounds how long `hull up` waits for a served site to answer
// before it stops blocking. First boot can genuinely take a few minutes: a
// WordPress image copies core into the (bind-mounted) webroot, a Laravel app
// runs its migrate hook and compiles Blade, and on Windows that all happens over
// a slow VM mount where a single first request can be tens of seconds.
const upReadyTimeout = 240 * time.Second

// reqTimeout is the per-request budget. It must be generous: on a slow Windows
// bind mount a Laravel app's very first HTTP request (autoloader crossing the
// mount, Blade compile, opcache prime) can take tens of seconds, and a short
// timeout would abort every probe and wrongly report the site as unresponsive.
const reqTimeout = 60 * time.Second

// readyPad clears leftover characters from the in-place progress line when it
// is overwritten by a shorter final line (kept simple: spaces, not ANSI, so it
// behaves the same on every Windows console).
const readyPad = "                              "

// reportUp prints the real outcome of starting a project. For a served site
// behind a running daemon it waits until the site actually responds, showing
// elapsed time, so `up` no longer claims success before the page loads. For
// everything else (unserved projects, apps/clusters, or the headless path
// where nothing is routed) it just confirms the containers started.
func reportUp(ctx context.Context, a *app, p *state.Project, viaDaemon bool) {
	m := p.Manifest
	// Only wait when the embedded router is actually serving this site: a
	// served site, behind a running daemon, with the router enabled. Otherwise
	// there is nothing to poll and we would just block for the full timeout.
	if m == nil || !viaDaemon || !a.Config.Router.Enabled || m.Type != manifest.TypeSite || !m.Served() {
		fmt.Printf("  started %s\n", p.Name)
		return
	}
	waitSiteReady(ctx, p.Name, "https://"+m.Domain+"."+a.Config.TLD)
}

// waitSiteReady polls url until the site answers or upReadyTimeout elapses. A
// 502/503/504 counts as "not ready yet" (Caddy returns 502 for a registered
// host whose backend has not booted); any other HTTP response means the app
// answered. In an interactive terminal it shows a live elapsed-time line.
func waitSiteReady(ctx context.Context, name, url string) {
	client := &http.Client{
		Timeout: reqTimeout,
		Transport: &http.Transport{
			// The probe checks reachability, not certificate trust: this CLI
			// process may not have Hull's local CA installed.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		// A redirect already proves the app answered; do not chase it.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx, cancel := context.WithTimeout(ctx, upReadyTimeout)
	defer cancel()

	start := time.Now()
	interactive := isInteractive()
	for {
		if reachable(ctx, client, url) {
			if interactive {
				fmt.Print("\r")
			}
			fmt.Printf("  ✔ %s is up at %s (%s)%s\n", name, url, elapsedSince(start), readyPad)
			return
		}
		select {
		case <-ctx.Done():
			if interactive {
				fmt.Print("\r")
			}
			fmt.Printf("  ! %s is still warming up after %s (a first boot over a slow mount can take longer). It is probably fine, not broken; watch it with `hull logs %s`.%s\n",
				name, upReadyTimeout.Round(time.Second), name, readyPad)
			return
		case <-time.After(1500 * time.Millisecond):
		}
		if interactive {
			fmt.Printf("\r  waiting for %s to respond... %s", name, elapsedSince(start))
		}
	}
}

// reachable reports whether url answered with anything other than a gateway
// error (which is Caddy telling us the backend is not up yet).
func reachable(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return false
	default:
		return true
	}
}

func elapsedSince(start time.Time) string {
	return time.Since(start).Round(time.Second).String()
}
