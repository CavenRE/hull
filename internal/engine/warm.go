package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/CavenRE/hull/internal/state"
)

// Warming a project means making the first request to it before the user does,
// so they never pay for it.
//
// The bill is real and it is large. PHP compiles every file it loads on the
// first request after a start, and on a Windows bind mount each of those files
// costs milliseconds to read, so a WordPress site can take 14 to 19 seconds to
// answer the first time and may surface as a router 502 while it does. Every
// request after that is served from the opcode cache.
//
// Hull already avoided this on one path by accident: the CLI polls the site
// after `hull up` and sits through the compile. Nothing else did. A project
// brought up by the daemon at login, by the GUI, or by `hull new` answered its
// first real visitor cold. This closes that gap for all of them.
//
// The request goes straight to the container's published loopback port with an
// explicit Host header, not through the router. That deliberately skips TLS,
// the local CA, the hosts-file block, and the route sync, none of which have to
// be correct for a warm-up to be worth doing, and any of which would otherwise
// turn a warm-up into a second thing that can fail.

const (
	// warmTimeout bounds one project's warm-up. Generous because that is the
	// entire point: a cold WordPress page on a slow mount takes 14 to 19
	// seconds, and giving up at 10 would warm nothing on exactly the machines
	// that need it most.
	warmTimeout = 90 * time.Second
	// warmRedirects is how many hops to follow. A framework's first response is
	// often a redirect (WordPress to its installer, Laravel to a canonical
	// host), and stopping there would compile the bootstrap and nothing else,
	// which is most of the cost left unpaid.
	warmRedirects = 3
	// warmConnectBudget is how long to keep trying to reach a container that is
	// starting. `compose up -d` returns before the application listens, so without
	// this the warm-up would race the boot and lose.
	warmConnectBudget = 60 * time.Second
	warmRetryInterval = 1 * time.Second
)

// WarmQuiet makes one request to each of a project's served endpoints so the
// next visitor gets a warm opcode cache. Errors are deliberately swallowed: a
// warm-up that fails costs a slow first page, which is exactly what the user
// had before, so it must never turn into a failed start.
func (e *Engine) WarmQuiet(ctx context.Context, p *state.Project) {
	if p == nil || p.Manifest == nil {
		return
	}
	eps := Endpoints(p.Manifest, e.Config.TLD)
	if len(eps) == 0 {
		return
	}
	client := &http.Client{
		Timeout: warmTimeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= warmRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	for _, ep := range eps {
		host := ""
		if len(ep.Hosts) > 0 {
			host = ep.Hosts[0]
		}
		e.warmEndpoint(ctx, client, p, ep, host)
	}
}

// warmEndpoint keeps trying until the container answers, then makes the one
// request that matters.
//
// The retry is the whole point rather than defensive padding. `compose up -d`
// returns when a container has STARTED, not when the application inside it is
// listening, and a PHP-FPM or Apache image takes a second or two more to get
// there. A single immediate request gets connection refused and warms exactly
// nothing, which is the silent way for this feature to look implemented and do
// no work at all.
func (e *Engine) warmEndpoint(ctx context.Context, client *http.Client, p *state.Project, ep Endpoint, host string) {
	deadline := time.Now().Add(warmConnectBudget)
	for {
		port, err := e.PublishedPort(ctx, p, ep.Service, ep.Port)
		if err == nil && port != 0 && warmOne(ctx, client, port, host) {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(warmRetryInterval):
		}
	}
}

// warmOne issues the single request and reports whether the container answered
// at all. A 500 still counts: the point is that PHP compiled and cached the
// request path, not that the application is happy.
//
// The Host header matters. A virtual-hosted app serves a different, and much
// cheaper, page for an unrecognised host, so warming without it can compile the
// wrong thing entirely.
func warmOne(ctx context.Context, client *http.Client, port int, host string) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	if host != "" {
		req.Host = host
	}
	resp, err := client.Do(req)
	if err != nil {
		return false // nothing listening yet, or it died mid-request
	}
	// Read the body to the end. The compile finishes when the response does,
	// not when the headers arrive, so discarding early would leave the job half
	// done on precisely the slow sites this exists for.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return true
}

// warmDetached warms a project without the caller waiting for it, but only
// where there is something left running to do the waiting.
//
// Both halves of that matter. In the daemon, detaching is required: it passes
// the HTTP request's context into Up, so an inline warm-up would delay the
// response the CLI is waiting on and die the moment the client hangs up, which
// is the normal end of a GUI-triggered start. In a one-shot CLI process the
// opposite is true: the command returns, the process exits, and a background
// goroutine is killed mid-request having warmed nothing. That is the worst of
// both, a feature that looks implemented and quietly does no work, so the CLI
// pays for it inline instead. It is the right place to pay: the person who
// typed `hull up` is waiting for a site they are about to open.
func (e *Engine) warmDetached(p *state.Project) {
	if !e.LongRunning {
		// A one-shot CLI process would kill this goroutine on exit, having warmed
		// nothing, and doing it inline instead would make `hull up` sit for up to
		// a minute waiting on a container that may never listen. Neither is worth
		// it, because the CLI already makes the request itself: waitSiteReady
		// polls the site after a start and sits through the compile. The daemon,
		// which has no such probe, is the one that needs this.
		return
	}
	project := *p // the goroutine outlives the caller's variable
	go func() {
		// Room for both halves: waiting for the container to listen, and then the
		// slow first request itself.
		ctx, cancel := context.WithTimeout(context.Background(), warmConnectBudget+warmTimeout)
		defer cancel()
		e.WarmQuiet(ctx, &project)
	}()
}
