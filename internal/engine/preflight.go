package engine

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/state"
)

// preflightPorts aborts an up when a fixed published host port the project
// wants is already held by something else. This is the adopted-cluster case:
// their compose publishes fixed host ports, so a stale listener (even a
// non-Hull one) silently aborts the bring-up and leaves containers wedged
// (the Sentinel report). Hull-rendered projects publish ephemeral loopback
// ports and never clash, so they're skipped. Best-effort: if ports can't be
// determined, or the project is already running, it just proceeds.
func (e *Engine) preflightPorts(ctx context.Context, p *state.Project) error {
	if !isCluster(p) || p.Manifest == nil {
		return nil
	}
	if running, err := dockerx.RunningComposeProjects(ctx); err == nil {
		for _, n := range running {
			if n == projectName(p) {
				return nil // already up , its own ports are expected to be held
			}
		}
	}
	composeDir := filepath.Join(p.Dir, p.Manifest.ComposeRoot)
	for _, hp := range publishedHostPorts(composeDir, p.Manifest.ComposeFiles) {
		if portHeld(hp.host, hp.port) {
			return fmt.Errorf("port %s is already in use , another process or stack holds it; stop it or remap the port, then retry", hp.addr())
		}
	}
	return nil
}

type hostPort struct {
	host string
	port int
}

func (h hostPort) addr() string {
	host := h.host
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(h.port))
}

func portHeld(host string, port int) bool {
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// publishedHostPorts extracts the fixed host ports a compose file publishes.
// Container-only ports ("80") and unparseable entries are skipped , only
// entries that bind a concrete host port can clash.
func publishedHostPorts(composeDir string, files []string) []hostPort {
	path := firstComposeFile(composeDir, files)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Services map[string]struct {
			Ports []hostPortSpec `yaml:"ports"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil
	}
	var out []hostPort
	for _, svc := range doc.Services {
		for _, ps := range svc.Ports {
			if ps.port != 0 {
				out = append(out, hostPort{host: ps.host, port: ps.port})
			}
		}
	}
	return out
}

// hostPortSpec decodes one compose ports entry, keeping the host IP and the
// published host port (short or long syntax).
type hostPortSpec struct {
	host string
	port int
}

func (p *hostPortSpec) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		p.host, p.port = parseShortHostPort(value.Value)
		return nil
	}
	var long struct {
		Published any    `yaml:"published"`
		HostIP    string `yaml:"host_ip"`
	}
	_ = value.Decode(&long)
	p.host = long.HostIP
	switch v := long.Published.(type) {
	case int:
		p.port = v
	case string:
		p.port, _ = strconv.Atoi(rangeStart(v))
	}
	return nil
}

// parseShortHostPort returns the host IP and published host port from a short
// compose mapping. "container" (no host port) yields ("", 0).
func parseShortHostPort(s string) (string, int) {
	s = strings.SplitN(s, "/", 2)[0]
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2: // host:container
		port, _ := strconv.Atoi(rangeStart(parts[0]))
		return "", port
	case 3: // ip:host:container
		port, _ := strconv.Atoi(rangeStart(parts[1]))
		return parts[0], port
	}
	return "", 0
}

func rangeStart(s string) string {
	return strings.SplitN(strings.TrimSpace(s), "-", 2)[0]
}
