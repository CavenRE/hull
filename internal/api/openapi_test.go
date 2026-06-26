package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestOpenAPICoversAllRoutes freezes the API contract: every route the server
// registers must be documented in docs/api/openapi.yaml, and vice versa. The
// registered routes are scraped from the source so the spec can't drift out of
// sync as endpoints are added or removed.
func TestOpenAPICoversAllRoutes(t *testing.T) {
	routeRE := regexp.MustCompile(`"(GET|POST|PUT|PATCH|DELETE) (/v1/[^"]*)"`)
	registered := map[string]bool{}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range routeRE.FindAllStringSubmatch(string(data), -1) {
			registered[m[1]+" "+m[2]] = true
		}
	}
	if len(registered) == 0 {
		t.Fatal("scraped no routes from source , the regex or layout changed")
	}

	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("reading openapi.yaml: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %v", err)
	}
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	documented := map[string]bool{}
	for path, item := range doc.Paths {
		for method := range item {
			if methods[method] {
				documented[strings.ToUpper(method)+" "+path] = true
			}
		}
	}

	for route := range registered {
		if !documented[route] {
			t.Errorf("route %q is registered but missing from openapi.yaml", route)
		}
	}
	for route := range documented {
		if !registered[route] {
			t.Errorf("route %q is documented but not registered (stale spec?)", route)
		}
	}
}
