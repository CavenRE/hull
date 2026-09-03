package engine

import (
	"context"
	"sort"
	"strconv"
	"sync"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/router"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// PortLookup resolves the host port docker assigned to a published container
// port. It takes the whole project (not just a dir) so the lookup can use the
// same pinned compose identity (-p/-f/--env-file) the project was brought up
// with , a bare dir-only lookup re-derives a different project name and 502s
// adopted clusters. Prod uses e.composeFor(p).Port; tests inject a fake.
type PortLookup func(ctx context.Context, p *state.Project, service string, containerPort int) (int, error)

// ComputeRoutes derives the router table from running projects: each
// routed service's loopback-published port becomes an upstream (ADR 0007).
// Projects whose ports cannot be resolved are skipped, not fatal , they
// may be mid-start.
func ComputeRoutes(ctx context.Context, projects []state.Project, tld string, running map[string]bool, ports PortLookup) []router.Route {
	// Collect every port lookup this route table needs, then resolve them
	// concurrently. Each lookup is a separate `docker compose port` shell-out,
	// so doing them serially made route-sync latency scale with the number of
	// served sites. Order-preserving so the output stays deterministic.
	type portReq struct {
		domains []string
		p       *state.Project
		service string
		cport   int
	}
	var reqs []portReq
	for i := range projects {
		p := &projects[i]
		if p.Manifest == nil || !running[p.Name] {
			continue
		}
		m := p.Manifest
		switch m.Type {
		case "cluster":
			// The host router only owns a cluster's vhosts in ingress: hull
			// mode. none = the cluster serves itself; delegate = the in-cluster
			// ingress container serves it (a separate loopback).
			if m.Ingress != manifest.IngressHull {
				continue
			}
			suffix := m.ClusterSuffix(tld)
			for _, key := range m.RouteKeys() {
				rt := m.Routes[key]
				if !rt.Served() {
					continue
				}
				reqs = append(reqs, portReq{domains: rt.Hosts(suffix), p: p, service: rt.Service, cport: rt.Port})
			}
		case "app":
			for _, key := range m.ContainerKeys() {
				c := m.Containers[key]
				if c.Domain == "" || !c.Served() {
					continue
				}
				upstream := c.Port
				if c.Template != "" {
					if def, ok := templates.Site(c.Template); ok && upstream == 0 {
						upstream = def.UpstreamPort
					}
				}
				reqs = append(reqs, portReq{domains: []string{c.Domain + "." + tld}, p: p, service: key, cport: upstream})
			}
		default: // site
			if !m.Served() {
				continue
			}
			def, ok := templates.Site(m.Template)
			if !ok {
				continue
			}
			reqs = append(reqs, portReq{domains: []string{m.Domain + "." + tld}, p: p, service: "app", cport: def.UpstreamPort})
		}
	}

	type portRes struct {
		port int
		err  error
	}
	results := make([]portRes, len(reqs))
	sem := make(chan struct{}, 8) // bound concurrent docker shell-outs
	var wg sync.WaitGroup
	for i := range reqs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			port, err := ports(ctx, reqs[i].p, reqs[i].service, reqs[i].cport)
			results[i] = portRes{port, err}
		}(i)
	}
	wg.Wait()

	var routes []router.Route
	for i := range reqs {
		if results[i].err != nil {
			// Internal-only service (no published port) or mid-start: skip, it
			// falls through to a readable 502 down-route.
			continue
		}
		for _, host := range reqs[i].domains {
			routes = append(routes, router.Route{Domain: host, Upstream: loopback(results[i].port)})
		}
	}
	return routes
}

func loopback(port int) string {
	return "127.0.0.1:" + strconv.Itoa(port)
}

// AllDomains lists every routed hostname of every managed project,
// running or not , the hosts-file block must cover stopped sites so they
// resolve the moment they start.
func AllDomains(projects []state.Project, tld string) []string {
	var domains []string
	for i := range projects {
		m := projects[i].Manifest
		if m == nil {
			continue
		}
		if m.Type == "cluster" {
			// Only ingress: hull clusters share the host router's domain set
			// (hosts entries + a vhost each). delegate is served by its own
			// container on a separate loopback; none is served by the cluster.
			if m.Ingress != manifest.IngressHull {
				continue
			}
			suffix := m.ClusterSuffix(tld)
			for _, key := range m.RouteKeys() {
				if rt := m.Routes[key]; rt.Served() {
					domains = append(domains, rt.Hosts(suffix)...)
				}
			}
			continue
		}
		if m.Type == "app" {
			for _, key := range m.ContainerKeys() {
				if c := m.Containers[key]; c.Domain != "" && c.Served() {
					domains = append(domains, c.Domain+"."+tld)
				}
			}
			continue
		}
		if !m.Served() {
			continue
		}
		domains = append(domains, m.Domain+"."+tld)
	}
	sort.Strings(domains)
	return domains
}

// Routes computes the live route table for this engine's config.
func (e *Engine) Routes(ctx context.Context) []router.Route {
	projects, err := state.Scan(e.Config.Roots, e.Config.Projects...)
	if err != nil {
		return nil
	}
	running := map[string]bool{}
	if names, err := dockerx.RunningComposeProjects(ctx); err == nil {
		for _, n := range names {
			running[n] = true
		}
	}
	// Resolve each project's published port through the SAME compose driver
	// (pinned -p/-f/--env-file) that Up used , see PortLookup / Compose.Port.
	ports := func(ctx context.Context, p *state.Project, service string, containerPort int) (int, error) {
		return e.composeFor(p).Port(ctx, service, containerPort)
	}
	return ComputeRoutes(ctx, projects, e.Config.TLD, running, ports)
}
