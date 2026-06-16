package engine

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/router"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// PortLookup resolves the host port docker assigned to a published
// container port (injectable for tests; dockerx.PublishedPort in prod).
type PortLookup func(ctx context.Context, dir, service string, containerPort int) (int, error)

// ComputeRoutes derives the router table from running projects: each
// routed service's loopback-published port becomes an upstream (ADR 0007).
// Projects whose ports cannot be resolved are skipped, not fatal — they
// may be mid-start.
func ComputeRoutes(ctx context.Context, projects []state.Project, tld string, running map[string]bool, ports PortLookup) []router.Route {
	var routes []router.Route
	for i := range projects {
		p := &projects[i]
		if p.Manifest == nil || !running[p.Name] {
			continue
		}
		m := p.Manifest
		switch m.Type {
		case "cluster":
			for _, key := range m.RouteKeys() {
				rt := m.Routes[key]
				if !rt.Served() {
					continue
				}
				if hostPort, err := ports(ctx, filepath.Join(p.Dir, m.ComposeRoot), rt.Service, rt.Port); err == nil {
					routes = append(routes, router.Route{
						Domain:   rt.Subdomain + "." + tld,
						Upstream: loopback(hostPort),
					})
				}
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
				if hostPort, err := ports(ctx, p.Dir, key, upstream); err == nil {
					routes = append(routes, router.Route{
						Domain:   c.Domain + "." + tld,
						Upstream: loopback(hostPort),
					})
				}
			}
		default: // site
			if !m.Served() {
				continue
			}
			def, ok := templates.Site(m.Template)
			if !ok {
				continue
			}
			if hostPort, err := ports(ctx, p.Dir, "app", def.UpstreamPort); err == nil {
				routes = append(routes, router.Route{
					Domain:   m.Domain + "." + tld,
					Upstream: loopback(hostPort),
				})
			}
		}
	}
	return routes
}

func loopback(port int) string {
	return "127.0.0.1:" + strconv.Itoa(port)
}

// AllDomains lists every routed hostname of every managed project,
// running or not — the hosts-file block must cover stopped sites so they
// resolve the moment they start.
func AllDomains(projects []state.Project, tld string) []string {
	var domains []string
	for i := range projects {
		m := projects[i].Manifest
		if m == nil {
			continue
		}
		if m.Type == "cluster" {
			for _, key := range m.RouteKeys() {
				if rt := m.Routes[key]; rt.Served() {
					domains = append(domains, rt.Subdomain+"."+tld)
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
	projects, err := state.Scan(e.Config.Roots)
	if err != nil {
		return nil
	}
	running := map[string]bool{}
	if names, err := dockerx.RunningComposeProjects(ctx); err == nil {
		for _, n := range names {
			running[n] = true
		}
	}
	return ComputeRoutes(ctx, projects, e.Config.TLD, running, dockerx.PublishedPort)
}
