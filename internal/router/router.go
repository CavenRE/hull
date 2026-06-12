package router

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/caddyserver/caddy/v2"

	// Standard Caddy modules: http server, reverse_proxy, tls, pki, ...
	_ "github.com/caddyserver/caddy/v2/modules/standard"
)

// Route maps one HTTPS hostname to a loopback upstream (ADR 0007).
type Route struct {
	// Domain is the full hostname, e.g. "myapp.test".
	Domain string
	// Upstream is the dial address, e.g. "127.0.0.1:55001".
	Upstream string
}

// Options configures the embedded router.
type Options struct {
	HTTPPort  int
	HTTPSPort int
	// DataDir stores the local CA and issued certificates
	// (<hullHome>/caddy).
	DataDir string
}

// CAName is the display name of Hull's local certificate authority.
const CAName = "Hull Local CA"

// ConfigJSON builds the full Caddy JSON config for a route set. Pure and
// deterministic (routes sorted by domain) for testability.
func ConfigJSON(routes []Route, o Options) ([]byte, error) {
	if o.HTTPPort == 0 {
		o.HTTPPort = 80
	}
	if o.HTTPSPort == 0 {
		o.HTTPSPort = 443
	}

	sorted := append([]Route(nil), routes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Domain < sorted[j].Domain })

	caddyRoutes := make([]map[string]any, 0, len(sorted))
	subjects := make([]string, 0, len(sorted))
	for _, r := range sorted {
		if r.Domain == "" || r.Upstream == "" {
			return nil, fmt.Errorf("invalid route %+v", r)
		}
		subjects = append(subjects, r.Domain)
		caddyRoutes = append(caddyRoutes, map[string]any{
			"match": []map[string]any{{"host": []string{r.Domain}}},
			"handle": []map[string]any{{
				"handler":   "reverse_proxy",
				"upstreams": []map[string]any{{"dial": r.Upstream}},
				"headers": map[string]any{
					"request": map[string]any{
						"set": map[string][]string{
							"X-Forwarded-Proto": {"https"},
						},
					},
				},
			}},
			"terminal": true,
		})
	}

	cfg := map[string]any{
		"admin": map[string]any{"disabled": true},
		"storage": map[string]any{
			"module": "file_system",
			"root":   o.DataDir,
		},
		"apps": map[string]any{
			"pki": map[string]any{
				"certificate_authorities": map[string]any{
					"local": map[string]any{
						"name":          CAName,
						"install_trust": false, // Hull manages trust itself (hull trust)
					},
				},
			},
			"tls": map[string]any{
				"automation": map[string]any{
					"policies": []map[string]any{{
						"subjects": subjects,
						"issuers":  []map[string]any{{"module": "internal", "ca": "local"}},
					}},
				},
			},
			"http": map[string]any{
				"http_port":  o.HTTPPort,
				"https_port": o.HTTPSPort,
				"servers": map[string]any{
					"hull": map[string]any{
						"listen": []string{fmt.Sprintf(":%d", o.HTTPSPort)},
						"routes": caddyRoutes,
					},
				},
			},
		},
	}
	return json.Marshal(cfg)
}

// Apply loads the config for the given routes into the embedded Caddy,
// starting it if needed. Reload is graceful (no dropped connections).
func Apply(routes []Route, o Options) error {
	cfg, err := ConfigJSON(routes, o)
	if err != nil {
		return err
	}
	return caddy.Load(cfg, true)
}

// Stop shuts the embedded Caddy down.
func Stop() error {
	return caddy.Stop()
}

// EnsureCA boots a minimal PKI-only config so the local root certificate
// materializes in DataDir without serving anything, then stops. Used by
// `hull trust` before the daemon has ever run.
func EnsureCA(dataDir string) error {
	cfg := map[string]any{
		"admin": map[string]any{"disabled": true},
		"storage": map[string]any{
			"module": "file_system",
			"root":   dataDir,
		},
		"apps": map[string]any{
			"pki": map[string]any{
				"certificate_authorities": map[string]any{
					"local": map[string]any{
						"name":          CAName,
						"install_trust": false,
					},
				},
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := caddy.Load(data, true); err != nil {
		return err
	}
	return caddy.Stop()
}
