package router

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/caddyserver/caddy/v2"

	// Register only the Caddy modules Hull's generated config actually
	// references, instead of the full `modules/standard` set (which drags in
	// every ACME issuer, DNS provider, and http middleware). This trims the
	// binary and the build. The router end-to-end tests (Apply + a live request,
	// EnsureCA, reload) fail if any referenced module is missing here.
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp"              // http app + static_response
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy" // reverse_proxy
	_ "github.com/caddyserver/caddy/v2/modules/caddypki"               // local CA (pki app)
	_ "github.com/caddyserver/caddy/v2/modules/caddytls"               // tls app + internal issuer
	_ "github.com/caddyserver/caddy/v2/modules/filestorage"            // file_system storage
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
	// BindHost is the loopback address to listen on (default 127.0.0.1).
	// A non-default 127.0.0.x lets Hull coexist with another local proxy
	// holding the same ports on a different loopback IP.
	BindHost string
}

// CAName is the display name of Hull's local certificate authority.
const CAName = "Hull Local CA"

// loopbackListen returns the loopback listen addresses for a port. Binding
// the specific loopback IP rather than ":port" avoids clashing with other
// local proxies bound to a different loopback address. IPv6 ::1 is added only
// for the default host, since a moved bind (127.0.0.x) has no v6 counterpart.
func loopbackListen(host string, port int) []string {
	if host == "" {
		host = "127.0.0.1"
	}
	addrs := []string{fmt.Sprintf("%s:%d", host, port)}
	if host == "127.0.0.1" {
		addrs = append(addrs, fmt.Sprintf("[::1]:%d", port))
	}
	return addrs
}

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
		if r.Domain == "" {
			return nil, fmt.Errorf("invalid route %+v", r)
		}
		subjects = append(subjects, r.Domain)
		var handle map[string]any
		if r.Upstream == "" {
			// A known host with no live backend (project stopped or crashed).
			// Answer a readable 502 instead of having no vhost at all , which
			// would fail the TLS handshake and surface as an opaque OS-level
			// error (curl SEC_E_ILLEGAL_MESSAGE). The subject still gets a
			// cert so HTTPS negotiates cleanly.
			handle = map[string]any{
				"handler":     "static_response",
				"status_code": 502,
				"headers":     map[string][]string{"Content-Type": {"text/plain; charset=utf-8"}},
				"body":        "Hull: this site is registered but not running.\nStart it with `hull up`, or check `hull status`.\n",
			}
		} else {
			handle = map[string]any{
				"handler":   "reverse_proxy",
				"upstreams": []map[string]any{{"dial": r.Upstream}},
				// Retry a just-started upstream for a few seconds instead of
				// returning 502 immediately, so a container that is still binding
				// its port right after `hull up` (or a browser hitting a cold
				// site) waits and succeeds rather than seeing a bad gateway.
				"load_balancing": map[string]any{
					"try_duration": "5s",
					"try_interval": "250ms",
				},
				"headers": map[string]any{
					"request": map[string]any{
						"set": map[string][]string{
							"X-Forwarded-Proto": {"https"},
						},
					},
				},
			}
		}
		caddyRoutes = append(caddyRoutes, map[string]any{
			"match":    []map[string]any{{"host": []string{r.Domain}}},
			"handle":   []map[string]any{handle},
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
						"issuers": []map[string]any{{
							"module": "internal",
							"ca":     "local",
							// Caddy's internal issuer defaults to 12-hour leaf
							// certificates. Those expire overnight (or across any
							// stop/start or sleep longer than the renewal window)
							// while the daemon is not running to renew them, which
							// surfaces as ERR_CERT_DATE_INVALID and, with HSTS, no
							// way to click through. Sign leaves directly with the
							// 10-year root (mkcert's model) and give them a 1-year
							// lifetime so a trusted cert stays valid across
							// restarts and sleeps. 365 days also stays under the
							// 398-day browser cap, and a locally-installed root is
							// exempt from it anyway.
							"lifetime":       "8760h",
							"sign_with_root": true,
						}},
					}},
				},
			},
			"http": map[string]any{
				"http_port":  o.HTTPPort,
				"https_port": o.HTTPSPort,
				"servers": map[string]any{
					"hull": map[string]any{
						// Bind loopback explicitly (v4 + v6), never 0.0.0.0:
						// keeps dev sites off the LAN and lets Hull coexist with
						// other local proxies bound to a different loopback IP
						// (e.g. a stack on 127.0.0.2) , an all-interfaces bind
						// would collide with those on the same port.
						"listen": loopbackListen(o.BindHost, o.HTTPSPort),
						// h3 would add a QUIC/UDP listener , pointless for
						// a loopback dev proxy, and Windows reserves large
						// UDP port ranges that make those binds flaky.
						"protocols": []string{"h1", "h2"},
						// We run our own redirect server below on the HTTP port.
						"automatic_https": map[string]any{"disable_redirects": true},
						"routes":          caddyRoutes,
					},
					"hull-http": map[string]any{
						"listen": loopbackListen(o.BindHost, o.HTTPPort),
						"routes": []map[string]any{{
							"handle": []map[string]any{{
								"handler":     "static_response",
								"status_code": 308,
								"headers": map[string][]string{
									"Location": {"https://{http.request.host}{http.request.uri}"},
								},
							}},
						}},
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
