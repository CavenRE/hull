package services

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CavenRE/hull/internal/compose"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/templates"
)

// Instance is one shared service instance (e.g. postgres-16).
type Instance struct {
	Name      string // postgres-16
	Engine    string // postgres
	Version   string // 16
	Container string // hull-postgres-16
	Dir       string
	Running   bool
	// HostPort is the stable loopback port the primary service port is
	// published on (0 = none) , what desktop tools connect to.
	HostPort int
}

// Manager owns shared service instances under <hullHome>/services. Each
// instance is its own compose project ("hull-<name>") whose container joins
// the shared (caddy) network, addressed by a fixed container name.
type Manager struct {
	HullHome string
	// Run executes attached commands (compose up/down); dockerx.Exec default.
	Run dockerx.Runner
	// Output captures command output (database creation); dockerx.Output default.
	Output func(ctx context.Context, dir, name string, args ...string) (string, error)
	// EnsureNet creates the shared network; dockerx.EnsureNetwork default.
	EnsureNet func(ctx context.Context, name string) error
	// RunningProjects lists running compose projects; dockerx default.
	RunningProjects func(ctx context.Context) ([]string, error)
}

// NewManager builds a production manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		HullHome:        cfg.HullHome,
		Run:             dockerx.Exec,
		Output:          dockerx.Output,
		EnsureNet:       dockerx.EnsureNetwork,
		RunningProjects: dockerx.RunningComposeProjects,
	}
}

// sharedNetwork is the external network instances and projects share.
// Reusing the caddy network keeps v1's Adminer able to reach databases.
const sharedNetwork = "caddy"

func (m *Manager) servicesDir() string {
	return filepath.Join(m.HullHome, "services")
}

// Dir returns the directory of an instance.
func (m *Manager) Dir(instance string) string {
	return filepath.Join(m.servicesDir(), instance)
}

// projectNameInvalid matches characters Docker strips from a compose project
// name (it normalizes to lowercase [a-z0-9_-]).
var projectNameInvalid = regexp.MustCompile(`[^a-z0-9_-]`)

// composeProject is the compose project name for an instance, normalized the
// same way Docker normalizes it: lowercase with characters outside [a-z0-9_-]
// removed. So instance "mysql-8.4" runs under project "hull-mysql-84" (the dot
// dropped). Matching Docker's normalization here is what makes running-detection
// in List line up with what Docker actually reports; the previous "hull-"+name
// never matched a dotted version, so those instances always showed as stopped.
func composeProject(instance string) string {
	return projectNameInvalid.ReplaceAllString(strings.ToLower("hull-"+instance), "")
}

// Resolve parses "engine" or "engine@version" into an engine def and
// effective version.
func Resolve(spec string) (templates.EngineDef, string, error) {
	engineName, version, _ := strings.Cut(spec, "@")
	def, ok := templates.Engine(engineName)
	if !ok {
		return templates.EngineDef{}, "", fmt.Errorf("unknown engine %q (built-ins: %s)", engineName, strings.Join(templates.EngineKeys(), ", "))
	}
	if version == "" {
		version = def.DefaultVersion
	}
	return def, version, nil
}

// Add renders and boots a shared instance, returning its name. Idempotent:
// an existing instance is just started.
func (m *Manager) Add(ctx context.Context, engineName, version string) (string, error) {
	def, version, err := Resolve(engineName + "@" + version)
	if err != nil {
		return "", err
	}
	name := templates.InstanceName(def.Name, version)
	dir := m.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	svc := &compose.ServiceDef{
		Image:         def.Image(version),
		ContainerName: templates.InstanceContainerName(def.Name, version),
		Command:       def.Command,
		Environment:   def.Env(""),
		Labels:        []string{compose.ManagedLabel},
		Networks:      []string{sharedNetwork},
	}
	if def.DataPath != "" {
		svc.Volumes = []string{"data:" + def.DataPath}
	}
	if def.Name == "adminer" {
		// Auto-login plugin: empty passwords are the local-dev norm.
		if err := templates.EnsureSystemFiles(m.HullHome); err != nil {
			return "", err
		}
		plugin := strings.ReplaceAll(templates.AdminerPluginPath(m.HullHome), "\\", "/")
		svc.Volumes = append(svc.Volumes, plugin+":/var/www/html/plugins-enabled/hull-login.php:ro")
		// servers.json backs the login-page database picker. Ensure it exists so
		// the bind mount resolves to a file (not a new dir); Hull regenerates it
		// whenever databases change.
		serversPath := strings.ReplaceAll(filepath.Join(m.HullHome, "system", "adminer", "servers.json"), "\\", "/")
		if _, err := os.Stat(serversPath); err != nil {
			_ = os.WriteFile(serversPath, []byte("[]"), 0o644)
		}
		svc.Volumes = append(svc.Volumes, serversPath+":/var/www/html/plugins-enabled/servers.json:ro")
	}

	// Primary port on a STABLE loopback host port: desktop tools
	// (TablePlus, DataGrip) connect here; survives instance restarts.
	if def.HostPortBase > 0 && def.DefaultPort() != "" {
		hostPort := existingHostPort(dir, def.DefaultPort())
		if hostPort == 0 {
			hostPort = m.pickStablePort(def.HostPortBase)
		}
		svc.Ports = append(svc.Ports, fmt.Sprintf("127.0.0.1:%d:%s", hostPort, def.DefaultPort()))
	}
	if def.UIPort > 0 {
		// Embedded web UI rides the host router (ADR 0007): publish on a
		// loopback ephemeral port; the daemon routes <UISubdomain>.<tld>.
		svc.Ports = append(svc.Ports, fmt.Sprintf("127.0.0.1::%d", def.UIPort))
	}

	file := &compose.File{
		Name:     composeProject(name),
		Services: map[string]*compose.ServiceDef{def.Name: svc},
		Networks: map[string]*compose.Network{sharedNetwork: {External: true}},
	}
	if def.DataPath != "" {
		file.Volumes = map[string]*compose.Volume{"data": nil}
	}
	data, err := compose.Marshal(file)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), data, 0o644); err != nil {
		return "", err
	}

	if err := m.EnsureNet(ctx, sharedNetwork); err != nil {
		return "", err
	}
	return name, m.Run(ctx, dir, "docker", "compose", "up", "-d")
}

// EnsureUp boots an existing instance (creating it if needed).
func (m *Manager) EnsureUp(ctx context.Context, engineName, version string) (string, error) {
	return m.Add(ctx, engineName, version)
}

// List returns all instances with running state, sorted by name.
func (m *Manager) List(ctx context.Context) ([]Instance, error) {
	entries, err := os.ReadDir(m.servicesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	running := map[string]bool{}
	if m.RunningProjects != nil {
		if names, err := m.RunningProjects(ctx); err == nil {
			for _, n := range names {
				running[n] = true
			}
		}
	}
	var instances []Instance
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		engineName, version, _ := strings.Cut(name, "-")
		in := Instance{
			Name:      name,
			Engine:    engineName,
			Version:   version,
			Container: "hull-" + name,
			Dir:       m.Dir(name),
			Running:   running[composeProject(name)],
		}
		if def, ok := templates.Engine(engineName); ok && def.DefaultPort() != "" {
			in.HostPort = existingHostPort(in.Dir, def.DefaultPort())
		}
		instances = append(instances, in)
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return instances, nil
}

// Start boots an existing instance.
func (m *Manager) Start(ctx context.Context, instance string) error {
	if err := m.exists(instance); err != nil {
		return err
	}
	return m.Run(ctx, m.Dir(instance), "docker", "compose", "up", "-d")
}

// Stop stops an instance (data preserved).
func (m *Manager) Stop(ctx context.Context, instance string) error {
	if err := m.exists(instance); err != nil {
		return err
	}
	return m.Run(ctx, m.Dir(instance), "docker", "compose", "down")
}

// Remove destroys an instance and its data volume.
func (m *Manager) Remove(ctx context.Context, instance string) error {
	if err := m.exists(instance); err != nil {
		return err
	}
	if err := m.Run(ctx, m.Dir(instance), "docker", "compose", "down", "-v"); err != nil {
		return err
	}
	return os.RemoveAll(m.Dir(instance))
}

// stablePortRE matches "127.0.0.1:HOST:CONTAINER" published ports.
var stablePortRE = regexp.MustCompile(`127\.0\.0\.1:(\d+):(\d+)`)

// existingHostPort returns the stable host port already persisted in an
// instance's compose file for the given container port , re-adding an
// instance must never move its port.
func existingHostPort(dir, containerPort string) int {
	data, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil {
		return 0
	}
	for _, match := range stablePortRE.FindAllStringSubmatch(string(data), -1) {
		if match[2] == containerPort {
			port, _ := strconv.Atoi(match[1])
			return port
		}
	}
	return 0
}

// pickStablePort scans upward from base for a port not claimed by another
// instance and currently bindable.
func (m *Manager) pickStablePort(base int) int {
	taken := map[int]bool{}
	if entries, err := os.ReadDir(m.servicesDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(m.Dir(e.Name()), "compose.yaml")); err == nil {
				for _, match := range stablePortRE.FindAllStringSubmatch(string(data), -1) {
					if p, err := strconv.Atoi(match[1]); err == nil {
						taken[p] = true
					}
				}
			}
		}
	}
	for port := base; port < base+200; port++ {
		if taken[port] {
			continue
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return port
	}
	return base // last resort; compose will surface the conflict
}

func (m *Manager) exists(instance string) error {
	if _, err := os.Stat(m.Dir(instance)); err != nil {
		return fmt.Errorf("no shared instance %q (add one with: hull services add <engine>[@version])", instance)
	}
	return nil
}

// CreateDatabase idempotently creates a database in a running instance.
func (m *Manager) CreateDatabase(ctx context.Context, engineName, version, database string) error {
	def, ok := templates.Engine(engineName)
	if !ok || !def.IsDatabase {
		return fmt.Errorf("engine %q does not host databases", engineName)
	}
	container := templates.InstanceContainerName(engineName, version)
	switch def.Name {
	case "postgres":
		_, err := m.Output(ctx, "", "docker", "exec", container,
			"psql", "-U", "postgres", "-c", fmt.Sprintf("CREATE DATABASE %q", database))
		if err != nil && strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return err
	case "mysql", "mariadb":
		cli := "mysql"
		if def.Name == "mariadb" {
			cli = "mariadb"
		}
		_, err := m.Output(ctx, "", "docker", "exec", container,
			cli, "-u", "root", "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", database))
		return err
	}
	return fmt.Errorf("unsupported engine %q", engineName)
}
