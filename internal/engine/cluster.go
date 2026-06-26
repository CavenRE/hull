package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/compose"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// ClusterOptions describes adopting an existing compose project as a Hull
// cluster (type: cluster) , Hull wraps it, it does not regenerate it.
type ClusterOptions struct {
	Dir          string   // the cluster project root (becomes the Hull project dir)
	Name         string   // optional; defaults to the slugified dir base name
	ComposeRoot  string   // dir holding the compose file, relative to Dir (default ".")
	ComposeFiles []string // extra -f files (optional)
	Profiles     []string // active profiles (optional)
}

var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// ContainerSpec is one container card from the create-cluster wizard.
type ContainerSpec struct {
	Name     string // service/container name (slugged)
	Template string // laravel|plain|wordpress, or "" for a raw image
	Image    string // raw image repo (when Template == "")
	Version  string // tag
	Port     int    // upstream/published port
	Serve    bool   // route a subdomain to it
	// Services are this card's linked infrastructure (db/redis/etc.).
	Services []ClusterServiceSpec
}

// ClusterServiceSpec is one linked service on a container card.
type ClusterServiceSpec struct {
	Engine  string
	Version string
}

// NewClusterOptions describes creating a NEW multi-container project.
type NewClusterOptions struct {
	Name        string
	Root        string // configured root to create under (default: first)
	ComposeRoot string // subfolder for the compose file (default ".")
	Managed     bool   // true: Hull renders+owns compose (type:app); false: write a compose you own (type:cluster)
	Containers  []ContainerSpec
	SkipStart   bool
}

// NewCluster scaffolds a new multi-container project, returning its dir.
// Managed → a Hull-rendered type:app; owned → a hand-editable compose file
// wrapped as type:cluster.
func (e *Engine) NewCluster(ctx context.Context, opts NewClusterOptions) (string, error) {
	name := manifest.Slug(opts.Name)
	if name == "" {
		return "", fmt.Errorf("invalid cluster name %q", opts.Name)
	}
	if len(opts.Containers) == 0 {
		return "", fmt.Errorf("a cluster needs at least one container")
	}
	root := opts.Root
	if root == "" {
		if len(e.Config.Roots) == 0 {
			return "", fmt.Errorf("no project roots configured")
		}
		root = e.Config.Roots[0]
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("target directory %s already exists", dir)
	}
	if opts.Managed {
		return e.newManagedCluster(ctx, dir, name, opts)
	}
	return e.newOwnedCluster(ctx, dir, name, opts)
}

func (e *Engine) newManagedCluster(ctx context.Context, dir, name string, opts NewClusterOptions) (string, error) {
	m := &manifest.Manifest{Schema: manifest.CurrentSchema, Name: name, Type: manifest.TypeApp, Containers: map[string]*manifest.Container{}}
	for _, c := range opts.Containers {
		key := manifest.Slug(c.Name)
		if key == "" {
			return "", fmt.Errorf("invalid container name %q", c.Name)
		}
		cont := &manifest.Container{Port: c.Port}
		if c.Template != "" {
			cont.Template = c.Template
		} else {
			cont.Image = imageRefFor(c)
		}
		serve := c.Serve
		cont.Serve = &serve
		if c.Serve {
			cont.Domain = key
		}
		m.Containers[key] = cont
		for _, sv := range c.Services {
			if sv.Engine == "" {
				continue
			}
			if m.Services == nil {
				m.Services = map[string]*manifest.Service{}
			}
			m.Services[key+"-"+sv.Engine] = &manifest.Service{Engine: sv.Engine, Version: sv.Version}
		}
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return "", err
	}
	if _, err := manifest.Parse(data); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := e.WriteArtifacts(m, dir); err != nil {
		return dir, err
	}
	if !opts.SkipStart {
		if err := templates.EnsureSystemFiles(e.Config.HullHome); err != nil {
			return dir, err
		}
		if err := e.prepareNetworks(ctx); err != nil {
			return dir, err
		}
		if err := e.compose(dir).Up(ctx); err != nil {
			return dir, err
		}
	}
	return dir, nil
}

func (e *Engine) newOwnedCluster(ctx context.Context, dir, name string, opts NewClusterOptions) (string, error) {
	composeRoot := opts.ComposeRoot
	if composeRoot == "" {
		composeRoot = "."
	}
	composeDir := filepath.Join(dir, composeRoot)

	f := &compose.File{Name: name, Services: map[string]*compose.ServiceDef{}}
	routes := map[string]*manifest.ClusterRoute{}
	for _, c := range opts.Containers {
		key := manifest.Slug(c.Name)
		if key == "" {
			return "", fmt.Errorf("invalid container name %q", c.Name)
		}
		sd := &compose.ServiceDef{Image: imageRefFor(c), Networks: []string{"default"}}
		if c.Port != 0 {
			sd.Ports = []string{fmt.Sprintf("127.0.0.1::%d", c.Port)}
		}
		f.Services[key] = sd
		if c.Serve && c.Port != 0 {
			routes[key] = &manifest.ClusterRoute{Service: key, Port: c.Port, Subdomain: key}
		}
		for _, sv := range c.Services {
			eng, ok := templates.Engine(sv.Engine)
			if !ok {
				continue
			}
			f.Services[key+"-"+sv.Engine] = &compose.ServiceDef{
				Image:       eng.Image(sv.Version),
				Command:     eng.Command,
				Environment: eng.Env(""),
				Networks:    []string{"default"},
			}
		}
	}
	if err := os.MkdirAll(composeDir, 0o755); err != nil {
		return "", err
	}
	cdata, err := yaml.Marshal(f)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(composeDir, "compose.yaml"), cdata, 0o644); err != nil {
		return "", err
	}

	m := &manifest.Manifest{Schema: manifest.CurrentSchema, Name: name, Type: manifest.TypeCluster, ComposeRoot: composeRoot, Routes: routes}
	mdata, err := yaml.Marshal(m)
	if err != nil {
		return dir, err
	}
	if _, err := manifest.Parse(mdata); err != nil {
		return dir, err
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.Filename), mdata, 0o644); err != nil {
		return dir, err
	}
	if !opts.SkipStart {
		if err := e.Up(ctx, &state.Project{Name: name, Dir: dir, Manifest: m}); err != nil {
			return dir, err
		}
	}
	return dir, nil
}

// imageRefFor resolves a container spec to a docker image reference.
func imageRefFor(c ContainerSpec) string {
	if c.Template != "" {
		if def, ok := templates.Site(c.Template); ok {
			return def.Image("", c.Version)
		}
	}
	if c.Image == "" {
		return ""
	}
	if c.Version == "" || c.Version == "latest" {
		return c.Image + ":latest"
	}
	return c.Image + ":" + c.Version
}

// AdoptCluster writes a type: cluster manifest into Dir, seeding routes from a
// Caddyfile in the compose root when present. It never renders or overwrites
// the user's compose files. Returns the written manifest.
func (e *Engine) AdoptCluster(opts ClusterOptions) (*manifest.Manifest, error) {
	dir := opts.Dir
	if dir == "" {
		return nil, fmt.Errorf("cluster directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	if _, err := os.Stat(filepath.Join(abs, manifest.Filename)); err == nil {
		return nil, fmt.Errorf("%s already has a hull.yaml", abs)
	}

	name := opts.Name
	if name == "" {
		name = manifest.Slug(filepath.Base(abs))
	}
	root := opts.ComposeRoot
	if root == "" {
		root = "."
	}
	composeDir := filepath.Join(abs, root)
	if !hasComposeFile(composeDir, opts.ComposeFiles) {
		return nil, fmt.Errorf("no compose file found in %s (pass --root or --compose)", composeDir)
	}

	// Seed routes from a Caddyfile if present; otherwise inspect the compose
	// services for web-looking published ports so adopt isn't blind.
	routes := parseCaddyRoutes(composeDir)
	if len(routes) == 0 {
		routes = parseComposeRoutes(composeDir, opts.ComposeFiles)
	}

	m := &manifest.Manifest{
		Schema:       manifest.CurrentSchema,
		Name:         name,
		Type:         manifest.TypeCluster,
		ComposeRoot:  root,
		ComposeFiles: opts.ComposeFiles,
		Profiles:     opts.Profiles,
		Routes:       routes,
	}

	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	// Round-trip to validate + normalize (subdomain defaults, etc.).
	if _, err := manifest.Parse(data); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(abs, manifest.Filename), data, 0o644); err != nil {
		return nil, err
	}
	return m, nil
}

func hasComposeFile(dir string, extra []string) bool {
	for _, f := range extra {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	for _, f := range composeNames {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

var (
	caddySiteRE  = regexp.MustCompile(`^([a-z0-9.-]+)\s*\{`)
	caddyProxyRE = regexp.MustCompile(`reverse_proxy\s+(?:[^\s]+\s+)*([a-z0-9_.-]+):(\d+)`)
)

// parseCaddyRoutes extracts a starting route map from a Caddyfile's vhost
// blocks. It's a best-effort seed (the proxy target is often a container_name,
// which the user may need to swap for the compose service name); empty when no
// Caddyfile is present.
func parseCaddyRoutes(composeDir string) map[string]*manifest.ClusterRoute {
	data, err := os.ReadFile(filepath.Join(composeDir, "Caddyfile"))
	if err != nil {
		return nil
	}
	routes := map[string]*manifest.ClusterRoute{}
	lines := strings.Split(string(data), "\n")
	var curSub string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := caddySiteRE.FindStringSubmatch(line); m != nil && strings.Contains(m[1], ".") {
			// Subdomain = the first DNS label of the site address.
			curSub = strings.SplitN(m[1], ".", 2)[0]
			continue
		}
		if curSub == "" {
			continue
		}
		if m := caddyProxyRE.FindStringSubmatch(line); m != nil {
			port, _ := strconv.Atoi(m[2])
			key := manifest.Slug(curSub)
			if key == "" {
				continue
			}
			routes[key] = &manifest.ClusterRoute{Service: m[1], Port: port, Subdomain: curSub}
			curSub = ""
		}
	}
	if len(routes) == 0 {
		return nil
	}
	return routes
}

// webPorts are the container ports that signal an HTTP service worth routing.
// Datastore ports (5432, 3306, 6379, …) are intentionally excluded so adopt
// doesn't seed routes for databases and caches.
var webPorts = map[int]bool{
	80: true, 443: true, 3000: true, 4200: true, 5000: true, 8000: true,
	8080: true, 8081: true, 8443: true, 8888: true, 9000: true,
}

// portSpec decodes one compose `ports` entry , short ("8080:80", "80/tcp",
// "127.0.0.1:8080:80") or long ({target: 80, published: 8080}) , keeping only
// the container (target) port.
type portSpec struct{ container int }

func (p *portSpec) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		p.container = shortPortTarget(value.Value)
		return nil
	}
	var long struct {
		Target int `yaml:"target"`
	}
	_ = value.Decode(&long)
	p.container = long.Target
	return nil
}

// shortPortTarget pulls the container port from a short-syntax mapping: the
// last colon-separated segment, minus any /protocol suffix.
func shortPortTarget(s string) int {
	s = strings.SplitN(s, "/", 2)[0]
	parts := strings.Split(s, ":")
	n, _ := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
	return n
}

// parseComposeRoutes seeds routes from the compose file when there's no
// Caddyfile: one route per service that publishes a web-looking port, keyed
// (and subdomained) by the service name. Best-effort , unparseable compose
// yields no routes rather than an error.
func parseComposeRoutes(composeDir string, files []string) map[string]*manifest.ClusterRoute {
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
			Ports []portSpec `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	routes := map[string]*manifest.ClusterRoute{}
	for name, svc := range doc.Services {
		for _, p := range svc.Ports {
			if !webPorts[p.container] {
				continue
			}
			key := manifest.Slug(name)
			if key == "" {
				break
			}
			routes[key] = &manifest.ClusterRoute{Service: name, Port: p.container, Subdomain: key}
			break // first web port wins
		}
	}
	if len(routes) == 0 {
		return nil
	}
	return routes
}

// firstComposeFile returns the path of the first existing compose file in dir
// (preferring explicitly-listed files), or "" if none.
func firstComposeFile(dir string, extra []string) string {
	for _, f := range append(append([]string{}, extra...), composeNames...) {
		p := filepath.Join(dir, f)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}
