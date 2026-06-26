package router

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CavenRE/hull/internal/certs"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestConfigJSONShape(t *testing.T) {
	cfg, err := ConfigJSON([]Route{
		{Domain: "b.test", Upstream: "127.0.0.1:2"},
		{Domain: "a.test", Upstream: "127.0.0.1:1"},
	}, Options{HTTPPort: 8080, HTTPSPort: 8443, DataDir: "/data"})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(cfg, &doc); err != nil {
		t.Fatal(err)
	}
	text := string(cfg)
	for _, want := range []string{
		`"https_port":8443`, `"http_port":8080`,
		`"host":["a.test"]`, `"dial":"127.0.0.1:1"`,
		`"install_trust":false`, `"X-Forwarded-Proto"`,
		// Loopback-only binding (coexists with other local proxies, off-LAN).
		`"127.0.0.1:8443"`, `"[::1]:8443"`, `"127.0.0.1:8080"`,
		`"disable_redirects":true`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("config missing %s", want)
		}
	}
	// Must never bind all interfaces.
	if strings.Contains(text, `":8443"`) || strings.Contains(text, `":8080"`) {
		t.Error("router binds all interfaces; expected loopback only")
	}
	// Deterministic: a.test sorts before b.test.
	if strings.Index(text, "a.test") > strings.Index(text, "b.test") {
		t.Error("routes not sorted")
	}
	// Rejects invalid routes.
	if _, err := ConfigJSON([]Route{{Domain: "", Upstream: "x"}}, Options{}); err == nil {
		t.Error("empty domain accepted")
	}
}

func TestConfigJSONDownHostRenders502(t *testing.T) {
	cfg, err := ConfigJSON([]Route{
		{Domain: "up.test", Upstream: "127.0.0.1:1"},
		{Domain: "down.test"}, // no upstream , project stopped
	}, Options{HTTPPort: 8080, HTTPSPort: 8443, DataDir: "/d"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(cfg)
	if !strings.Contains(text, `"status_code":502`) {
		t.Error("a host with no upstream should render a 502 static_response")
	}
	if !strings.Contains(text, `"host":["down.test"]`) {
		t.Error("down host vhost missing")
	}
	// Both hosts must still get a TLS subject so HTTPS negotiates (no handshake
	// alert): the policy lists both subjects.
	if !strings.Contains(text, "up.test") || !strings.Contains(text, "down.test") {
		t.Error("down host should still appear as a TLS subject")
	}
}

func TestBindHostListen(t *testing.T) {
	cfg, err := ConfigJSON([]Route{{Domain: "a.test", Upstream: "127.0.0.1:1"}},
		Options{HTTPPort: 8080, HTTPSPort: 8443, DataDir: "/d", BindHost: "127.0.0.3"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(cfg)
	if !strings.Contains(text, `"127.0.0.3:8443"`) || !strings.Contains(text, `"127.0.0.3:8080"`) {
		t.Errorf("bind host not honored:\n%s", text)
	}
	// A moved bind has no IPv6 loopback peer.
	if strings.Contains(text, `::1`) {
		t.Error("moved bind must not add IPv6 ::1")
	}
}

func TestEmbeddedRouterEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "hello from upstream; proto=%s", r.Header.Get("X-Forwarded-Proto"))
	}))
	defer upstream.Close()

	opts := Options{
		HTTPPort:  freePort(t),
		HTTPSPort: freePort(t),
		DataDir:   t.TempDir(),
	}
	route := Route{Domain: "demo.test", Upstream: strings.TrimPrefix(upstream.URL, "http://")}
	if err := Apply([]Route{route}, opts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })

	// Client that resolves demo.test to the router's HTTPS port and
	// accepts the internal CA (we don't install trust in tests).
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, fmt.Sprintf("127.0.0.1:%d", opts.HTTPSPort))
			},
		},
	}

	var lastErr error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("https://demo.test/")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			if !strings.Contains(string(body), "hello from upstream") {
				t.Fatalf("body = %s", body)
			}
			if !strings.Contains(string(body), "proto=https") {
				t.Fatalf("X-Forwarded-Proto not set: %s", body)
			}
			// CA root must have materialized for `hull trust`.
			if !certs.Trusted(opts.DataDir) {
				t.Error("root.crt not written to data dir")
			}
			return
		}
		lastErr = err
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("router never served: %v", lastErr)
}

func TestApplyReloadChangesRoutes(t *testing.T) {
	up1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "one")
	}))
	defer up1.Close()
	up2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "two")
	}))
	defer up2.Close()

	opts := Options{HTTPPort: freePort(t), HTTPSPort: freePort(t), DataDir: t.TempDir()}
	get := func() string {
		client := &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, fmt.Sprintf("127.0.0.1:%d", opts.HTTPSPort))
				},
			},
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := client.Get("https://reload.test/")
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				return string(body)
			}
			time.Sleep(300 * time.Millisecond)
		}
		return "<timeout>"
	}

	if err := Apply([]Route{{Domain: "reload.test", Upstream: strings.TrimPrefix(up1.URL, "http://")}}, opts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	if got := get(); got != "one" {
		t.Fatalf("before reload: %q", got)
	}
	if err := Apply([]Route{{Domain: "reload.test", Upstream: strings.TrimPrefix(up2.URL, "http://")}}, opts); err != nil {
		t.Fatal(err)
	}
	if got := get(); got != "two" {
		t.Fatalf("after reload: %q", got)
	}
}

func TestEnsureCA(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCA(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(certs.RootCertPath(dir)); err != nil {
		t.Fatalf("root.crt not provisioned: %v", err)
	}
}
