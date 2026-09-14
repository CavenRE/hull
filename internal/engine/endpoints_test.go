package engine

import (
	"testing"

	"github.com/CavenRE/hull/internal/manifest"
)

func parseManifest(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

func TestEndpointsSite(t *testing.T) {
	m := parseManifest(t, "schema: 1\nname: blog\ntype: site\ntemplate: wordpress\ndomain: blog\nservices:\n  db:\n    engine: mariadb\n")
	eps := Endpoints(m, "test")
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1: %+v", len(eps), eps)
	}
	if eps[0].Service != "app" {
		t.Errorf("service = %q, want app", eps[0].Service)
	}
	if eps[0].Port != 80 {
		t.Errorf("port = %d, want the wordpress image's 80", eps[0].Port)
	}
	if len(eps[0].Hosts) != 1 || eps[0].Hosts[0] != "blog.test" {
		t.Errorf("hosts = %v, want [blog.test]", eps[0].Hosts)
	}
}

// An unserved project has no published port at all, so anything that asks for
// its URL or tries to warm it has to get nothing rather than a bad guess.
func TestEndpointsUnservedSite(t *testing.T) {
	m := parseManifest(t, "schema: 1\nname: blog\ntype: site\ntemplate: wordpress\ndomain: blog\nserve: false\nservices:\n  db:\n    engine: mariadb\n")
	if eps := Endpoints(m, "test"); len(eps) != 0 {
		t.Errorf("got %+v, want no endpoints for an unserved site", eps)
	}
}

func TestEndpointsUnknownTemplate(t *testing.T) {
	m := &manifest.Manifest{Schema: 1, Name: "x", Type: "site", Template: "nonesuch", Domain: "x"}
	if eps := Endpoints(m, "test"); len(eps) != 0 {
		t.Errorf("got %+v, want no endpoints when the template is unknown", eps)
	}
}

func TestEndpointsNilManifest(t *testing.T) {
	if eps := Endpoints(nil, "test"); eps != nil {
		t.Errorf("got %+v, want nil for a project with no manifest", eps)
	}
}

// An app's container port comes from the manifest when set and from its
// template otherwise, and only served containers count.
func TestEndpointsApp(t *testing.T) {
	m := parseManifest(t, "schema: 1\nname: stack\ntype: app\ncontainers:\n"+
		"  web:\n    template: plain\n    domain: web\n"+
		"  api:\n    image: nginx\n    domain: api\n    port: 9000\n"+
		"  worker:\n    image: nginx\n")
	eps := Endpoints(m, "test")
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2 (worker has no domain): %+v", len(eps), eps)
	}
	byService := map[string]Endpoint{}
	for _, ep := range eps {
		byService[ep.Service] = ep
	}
	if got := byService["web"].Port; got != 8080 {
		t.Errorf("web port = %d, want the plain template's 8080", got)
	}
	if got := byService["api"].Port; got != 9000 {
		t.Errorf("api port = %d, want the manifest's 9000", got)
	}
	if got := byService["api"].Hosts; len(got) != 1 || got[0] != "api.test" {
		t.Errorf("api hosts = %v, want [api.test]", got)
	}
}

// Hull only owns a cluster's vhosts in ingress: hull mode. In the other modes
// something else serves them, and Hull pointing at them would be wrong.
func TestEndpointsClusterIngressModes(t *testing.T) {
	clusterSrc := func(ingress string) string {
		return "schema: 1\nname: c\ntype: cluster\ncompose_root: .\ningress: " + ingress +
			"\nroutes:\n  edge:\n    service: edge\n    port: 8080\n    subdomain: shop\n"
	}
	eps := Endpoints(parseManifest(t, clusterSrc("hull")), "test")
	if len(eps) != 1 {
		t.Fatalf("ingress hull: got %d endpoints, want 1", len(eps))
	}
	if eps[0].Service != "edge" || eps[0].Port != 8080 {
		t.Errorf("got %+v, want the route's own service and port", eps[0])
	}
	// delegate means an in-cluster ingress container serves the vhosts on its
	// own loopback, so the host router must stay out of it.
	if got := Endpoints(parseManifest(t, clusterSrc("delegate")), "test"); len(got) != 0 {
		t.Errorf("ingress delegate: got %+v, want none (the cluster serves those itself)", got)
	}
}
