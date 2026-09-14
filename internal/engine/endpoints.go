package engine

import (
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/templates"
)

// Endpoint is one HTTP-serving service inside a project: which compose service
// it is, which port it listens on inside its container, and the hostnames the
// router answers for it.
//
// Working this out is fiddly and easy to get subtly wrong (an app's container
// port can come from either the manifest or its template, a cluster only counts
// when Hull owns its ingress), so it lives in one place. The router builds its
// table from this, `hull url` prints it, and the warm-up request aims at it; all
// three agreeing is what stops "the URL Hull printed" and "the thing Hull
// actually serves" drifting apart.
type Endpoint struct {
	// Service is the compose service key.
	Service string
	// Port is the port inside the container, not the published host port.
	Port int
	// Hosts are the fully-qualified names the router serves this on.
	Hosts []string
}

// Endpoints returns the HTTP endpoints a project serves, in manifest order.
// Returns nothing for a project that serves no HTTP (serve: false, a cluster
// that handles its own ingress, a manifest Hull cannot read).
func Endpoints(m *manifest.Manifest, tld string) []Endpoint {
	if m == nil {
		return nil
	}
	var eps []Endpoint
	switch m.Type {
	case "cluster":
		// The host router only owns a cluster's vhosts in ingress: hull mode.
		// none means the cluster serves itself; delegate means an in-cluster
		// ingress container does, on its own loopback.
		if m.Ingress != manifest.IngressHull {
			return nil
		}
		suffix := m.ClusterSuffix(tld)
		for _, key := range m.RouteKeys() {
			rt := m.Routes[key]
			if !rt.Served() {
				continue
			}
			eps = append(eps, Endpoint{Service: rt.Service, Port: rt.Port, Hosts: rt.Hosts(suffix)})
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
			eps = append(eps, Endpoint{Service: key, Port: upstream, Hosts: []string{c.Domain + "." + tld}})
		}
	default: // site
		if !m.Served() {
			return nil
		}
		def, ok := templates.Site(m.Template)
		if !ok {
			return nil
		}
		eps = append(eps, Endpoint{Service: "app", Port: def.UpstreamPort, Hosts: []string{m.Domain + "." + tld}})
	}
	return eps
}
