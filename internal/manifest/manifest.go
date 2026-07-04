package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/templates"
)

// CurrentSchema is the manifest schema version this build reads and writes.
const CurrentSchema = 1

// Filename is the canonical manifest file name inside a project directory.
const Filename = "hull.yaml"

// Type distinguishes a single-web-container site from a multi-container app.
type Type string

const (
	TypeSite    Type = "site"
	TypeApp     Type = "app"
	TypeCluster Type = "cluster"
)

// Mode says whether a service is a project-private container or a link to a
// shared global instance (shared instances arrive in Phase 5).
type Mode string

const (
	ModeDedicated Mode = "dedicated"
	ModeShared    Mode = "shared"
)

// Manifest is the source of truth for a Hull project (ADR 0003).
type Manifest struct {
	Schema   int    `yaml:"schema"`
	Name     string `yaml:"name"`
	Type     Type   `yaml:"type,omitempty"`
	Template string `yaml:"template,omitempty"` // sites only
	Domain   string `yaml:"domain,omitempty"`   // sites only; defaults to Name
	PHP      string `yaml:"php,omitempty"`      // sites only (laravel/plain)
	Version  string `yaml:"version,omitempty"`  // sites only (wordpress image tag)
	// Serve controls whether Hull gives this project a routed domain (vhost
	// + cert + DNS). nil = heuristic default; explicit wins. (Wired in J1.)
	Serve *bool `yaml:"serve,omitempty"`

	Containers map[string]*Container `yaml:"containers,omitempty"` // apps only
	Services   map[string]*Service   `yaml:"services,omitempty"`
	Env        map[string]string     `yaml:"env,omitempty"`
	// EnvFile points docker compose at an env file (path relative to the
	// project dir) for ${VAR} interpolation , mainly for adopted clusters whose
	// compose sits in a subdir and would otherwise miss a repo-root .env.
	EnvFile string `yaml:"env_file,omitempty"`
	Hooks   Hooks  `yaml:"hooks,omitempty"`

	// Cluster fields (type: cluster) , Hull wraps an existing compose project
	// rather than generating one. Orchestration stays with docker compose.
	ComposeRoot  string                   `yaml:"compose_root,omitempty"`  // dir holding the compose file, relative to the project (default ".")
	ComposeFiles []string                 `yaml:"compose_files,omitempty"` // extra -f files (base auto-detected if empty)
	Profiles     []string                 `yaml:"profiles,omitempty"`      // active compose profiles
	Routes       map[string]*ClusterRoute `yaml:"routes,omitempty"`        // served subdomains → service:port
	// BaseDomain is the domain cluster routes nest under (e.g. "tapkit.local").
	// Empty means routes use Hull's TLD (<subdomain>.<tld>). A verbatim base is
	// kept as-is, for a cluster whose apps hardcode their own domain.
	BaseDomain string `yaml:"base_domain,omitempty"`
	// Ingress selects how Hull serves the cluster's URLs: "" (none: observe and
	// list only), "delegate" (Hull fronts loopback + TLS, forwards to the
	// cluster's own gateway), or "hull" (Hull owns every vhost).
	Ingress string `yaml:"ingress,omitempty"`
}

// Cluster ingress modes for Manifest.Ingress.
const (
	IngressNone     = ""         // observe and list only; the cluster serves itself
	IngressDelegate = "delegate" // Hull fronts loopback + TLS, forwards to the cluster gateway
	IngressHull     = "hull"     // Hull owns every vhost, proxying to services by name
)

// ClusterRoute maps a subdomain to one of the cluster's compose services.
type ClusterRoute struct {
	Service   string   `yaml:"service"`             // compose service name
	Port      int      `yaml:"port"`                // container port to proxy
	Subdomain string   `yaml:"subdomain,omitempty"` // defaults to the route key
	Aliases   []string `yaml:"aliases,omitempty"`   // extra subdomain labels for the same service
	Serve     *bool    `yaml:"serve,omitempty"`     // nil = served
}

// Served reports whether this route gets a routed domain (default true).
func (r *ClusterRoute) Served() bool {
	if r == nil {
		return false
	}
	if r.Serve != nil {
		return *r.Serve
	}
	return true
}

// Hosts returns a route's fully-qualified hostnames (its subdomain plus any
// aliases) under the given domain suffix, e.g. "api.tapkit.local".
func (r *ClusterRoute) Hosts(suffix string) []string {
	if r == nil {
		return nil
	}
	hosts := make([]string, 0, 1+len(r.Aliases))
	if r.Subdomain != "" {
		hosts = append(hosts, r.Subdomain+"."+suffix)
	}
	for _, a := range r.Aliases {
		if a != "" {
			hosts = append(hosts, a+"."+suffix)
		}
	}
	return hosts
}

// ClusterSuffix is the domain that cluster routes nest under: BaseDomain when
// set, otherwise the given TLD.
func (m *Manifest) ClusterSuffix(tld string) string {
	if m.BaseDomain != "" {
		return m.BaseDomain
	}
	return tld
}

// Container is one container of a type:app project. Exactly one source must
// be set: a Hull template, or a raw image/build context.
type Container struct {
	Template string            `yaml:"template,omitempty"`
	Image    string            `yaml:"image,omitempty"`
	Build    string            `yaml:"build,omitempty"`
	PHP      string            `yaml:"php,omitempty"`
	Version  string            `yaml:"version,omitempty"`
	Path     string            `yaml:"path,omitempty"` // host dir mounted as webroot (template containers); default ./
	Domain   string            `yaml:"domain,omitempty"`
	Port     int               `yaml:"port,omitempty"` // upstream port; required for routed raw containers
	Command  string            `yaml:"command,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
	// Serve controls whether this container gets a routed domain. nil =
	// heuristic default (a domain/port implies served).
	Serve *bool `yaml:"serve,omitempty"`
}

// Service declares infrastructure the project needs (database, cache).
// The map key becomes the compose service name and in-network hostname.
type Service struct {
	Engine   string `yaml:"engine"`
	Version  string `yaml:"version,omitempty"`
	Mode     Mode   `yaml:"mode,omitempty"`
	Database string `yaml:"database,omitempty"`
	// Serve controls whether a service with a web UI is routed. nil =
	// heuristic (UISubdomain engines served, plain datastores not).
	Serve *bool `yaml:"serve,omitempty"`
}

// Hooks are project lifecycle commands run inside a container at well-defined
// points. Each event is a list of Hook; the engine runs them in order.
// post_* events that follow a start (create/up/rebuild/reset/import) are
// readiness-gated (retried) so a "release" step like a DB migration can wait
// for its dependencies.
type Hooks struct {
	PostCreate  []Hook `yaml:"post_create,omitempty"`
	PostImport  []Hook `yaml:"post_import,omitempty"`
	PreUp       []Hook `yaml:"pre_up,omitempty"`
	PostUp      []Hook `yaml:"post_up,omitempty"`
	PreDown     []Hook `yaml:"pre_down,omitempty"`
	PostRebuild []Hook `yaml:"post_rebuild,omitempty"`
	PostReset   []Hook `yaml:"post_reset,omitempty"`
}

// Hook is one lifecycle command. It accepts two YAML forms: a bare string
// ("php artisan migrate --force"), or a mapping with a target service and run
// policy:
//
//   - run: sentinelctl keys init
//     service: sentinel
//     when: once            # always (default) | once | changed
//     ignore_failure: false
type Hook struct {
	Run           string `yaml:"run"`
	Service       string `yaml:"service,omitempty"`
	When          string `yaml:"when,omitempty"`
	IgnoreFailure bool   `yaml:"ignore_failure,omitempty"`
}

// UnmarshalYAML accepts either a scalar (the command) or a full mapping.
func (h *Hook) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		h.Run = value.Value
		return nil
	}
	type raw Hook
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	*h = Hook(r)
	return nil
}

var (
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	keyRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	domainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	phpRE    = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ValidSubdomain reports whether s is a usable cluster route subdomain label
// (lowercase letters, digits, hyphens; starts with a letter).
func ValidSubdomain(s string) bool {
	return nameRE.MatchString(s)
}

// Slug normalizes a display name to a domain-safe label matching nameRE:
// lowercase, spaces/underscores/dots → hyphens, other chars dropped,
// collapsed and trimmed hyphens. The result may still start with a digit
// (rare); validation reports that with a friendly message.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '_' || r == '-' || r == '.':
			if b.Len() > 0 && !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// Served reports whether an app container is routed. Explicit Serve wins;
// the default is "served when it declares a domain".
func (c *Container) Served() bool {
	if c == nil {
		return false
	}
	if c.Serve != nil {
		return *c.Serve
	}
	return c.Domain != ""
}

// Served reports whether Hull should give this project a routed domain
// (vhost + cert + DNS). Explicit Serve wins; the default is true , sites are
// web-facing. Workers/headless apps set serve: false.
func (m *Manifest) Served() bool {
	if m.Serve != nil {
		return *m.Serve
	}
	return true
}

// Load reads, normalizes, and validates the manifest at path, which may be a
// project directory (containing hull.yaml) or the manifest file itself.
func Load(path string) (*Manifest, error) {
	file := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		file = filepath.Join(path, Filename)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return m, nil
}

// Parse decodes a manifest strictly (unknown fields are errors), applies
// defaults, and validates.
func Parse(data []byte) (*Manifest, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parsing: %w", err)
	}
	m.normalize()
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// normalize applies defaults in place. It is idempotent.
func (m *Manifest) normalize() {
	if m.Type == "" {
		m.Type = TypeSite
	}
	if m.Type == TypeSite && m.Domain == "" {
		m.Domain = m.Name
	}
	if m.Type == TypeSite && m.PHP == "" && m.Template != "wordpress" {
		m.PHP = templates.DefaultPHP
	}
	for _, c := range m.Containers {
		if c == nil {
			continue
		}
		if c.Path == "" {
			c.Path = "./"
		}
		if c.Template != "" && c.Template != "wordpress" && c.PHP == "" {
			c.PHP = templates.DefaultPHP
		}
	}
	if m.Type == TypeCluster {
		if m.ComposeRoot == "" {
			m.ComposeRoot = "."
		}
		for key, rt := range m.Routes {
			if rt != nil && rt.Subdomain == "" {
				rt.Subdomain = key
			}
		}
	}
	for _, s := range m.Services {
		if s == nil {
			continue
		}
		s.Engine = strings.ToLower(s.Engine)
		if s.Mode == "" {
			s.Mode = ModeDedicated
		}
		if eng, ok := templates.Engine(s.Engine); ok {
			if s.Version == "" {
				s.Version = eng.DefaultVersion
			}
			if eng.IsDatabase && s.Database == "" {
				s.Database = strings.ReplaceAll(m.Name, "-", "_")
			}
		}
	}
}

// Validate checks the manifest for structural and semantic errors. All
// problems found are reported together.
func (m *Manifest) Validate() error {
	var errs []error
	fail := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf(format, a...))
	}

	switch {
	case m.Schema == 0:
		fail("missing 'schema' field (add: schema: %d)", CurrentSchema)
	case m.Schema > CurrentSchema:
		fail("manifest schema %d is newer than this Hull understands (max %d) , upgrade Hull", m.Schema, CurrentSchema)
	case m.Schema < 0 || m.Schema < CurrentSchema:
		fail("unknown manifest schema %d", m.Schema)
	}

	if m.Name == "" {
		fail("'name' is required")
	} else if !nameRE.MatchString(m.Name) || len(m.Name) > 63 {
		fail("invalid name %q: lowercase letters, digits, and hyphens only, starting with a letter", m.Name)
	}

	if m.Type != TypeCluster && (m.BaseDomain != "" || m.Ingress != "") {
		fail("'base_domain' and 'ingress' are only valid for type: cluster")
	}

	switch m.Type {
	case TypeSite:
		m.validateSite(fail)
	case TypeApp:
		m.validateApp(fail)
	case TypeCluster:
		m.validateCluster(fail)
	default:
		fail("invalid type %q: must be %q, %q, or %q", m.Type, TypeSite, TypeApp, TypeCluster)
	}

	m.validateServices(fail)
	m.validateHooks(fail)

	for key := range m.Env {
		if !envKeyRE.MatchString(key) {
			fail("invalid env key %q", key)
		}
	}

	return errors.Join(errs...)
}

// validateHooks checks every lifecycle hook list: a command is required, and
// when:-policy must be one of the known values.
func (m *Manifest) validateHooks(fail func(string, ...any)) {
	events := map[string][]Hook{
		"post_create":  m.Hooks.PostCreate,
		"post_import":  m.Hooks.PostImport,
		"pre_up":       m.Hooks.PreUp,
		"post_up":      m.Hooks.PostUp,
		"pre_down":     m.Hooks.PreDown,
		"post_rebuild": m.Hooks.PostRebuild,
		"post_reset":   m.Hooks.PostReset,
	}
	for event, hooks := range events {
		for i, h := range hooks {
			if strings.TrimSpace(h.Run) == "" {
				fail("hook %s[%d]: 'run' command is required", event, i)
			}
			switch h.When {
			case "", "always", "once", "changed":
			default:
				fail("hook %s[%d]: invalid when %q (use always, once, or changed)", event, i, h.When)
			}
		}
	}
}

func (m *Manifest) validateSite(fail func(string, ...any)) {
	if len(m.Containers) > 0 {
		fail("'containers' is only valid for type: app")
	}
	if m.Template == "" {
		fail("'template' is required for type: site")
	} else if _, ok := templates.Site(m.Template); !ok {
		fail("unknown template %q (built-ins: %s)", m.Template, strings.Join(templates.SiteKeys(), ", "))
	}
	if m.Domain != "" && !domainRE.MatchString(m.Domain) {
		fail("invalid domain %q", m.Domain)
	}
	if m.PHP != "" && !phpRE.MatchString(m.PHP) {
		fail("invalid php version %q (expected e.g. \"8.3\")", m.PHP)
	}
	if def, ok := templates.Site(m.Template); ok && len(def.RequiredDB) > 0 {
		if !m.hasServiceEngine(def.RequiredDB...) {
			fail("template %q requires a database service with engine %s",
				m.Template, strings.Join(def.RequiredDB, " or "))
		}
	}
}

func (m *Manifest) validateApp(fail func(string, ...any)) {
	if m.Template != "" || m.PHP != "" || m.Version != "" || m.Domain != "" {
		fail("'template', 'php', 'version', and 'domain' are container-level fields for type: app")
	}
	if len(m.Containers) == 0 {
		fail("'containers' is required for type: app")
	}
	for key, c := range m.Containers {
		if !keyRE.MatchString(key) {
			fail("invalid container key %q", key)
			continue
		}
		if c == nil {
			fail("container %q is empty", key)
			continue
		}
		sources := 0
		if c.Template != "" {
			sources++
		}
		if c.Image != "" || c.Build != "" {
			sources++
		}
		if sources == 0 {
			fail("container %q needs one of: template, image, build", key)
		}
		if c.Template != "" && (c.Image != "" || c.Build != "") {
			fail("container %q: template cannot be combined with image/build", key)
		}
		if c.Template != "" {
			if _, ok := templates.Site(c.Template); !ok {
				fail("container %q: unknown template %q", key, c.Template)
			}
		}
		if c.Domain != "" {
			if !domainRE.MatchString(c.Domain) {
				fail("container %q: invalid domain %q", key, c.Domain)
			}
			if c.Template == "" && c.Port == 0 {
				fail("container %q: 'port' is required when a raw container has a domain", key)
			}
		}
		if c.Port < 0 || c.Port > 65535 {
			fail("container %q: invalid port %d", key, c.Port)
		}
		if c.PHP != "" && !phpRE.MatchString(c.PHP) {
			fail("container %q: invalid php version %q", key, c.PHP)
		}
		for envKey := range c.Env {
			if !envKeyRE.MatchString(envKey) {
				fail("container %q: invalid env key %q", key, envKey)
			}
		}
	}
}

func (m *Manifest) validateCluster(fail func(string, ...any)) {
	if m.Template != "" || m.PHP != "" || m.Version != "" || m.Domain != "" {
		fail("'template', 'php', 'version', and 'domain' are not valid for type: cluster")
	}
	if len(m.Containers) > 0 {
		fail("'containers' is not valid for type: cluster (it wraps an existing compose project)")
	}
	if len(m.Services) > 0 {
		fail("'services' is not valid for type: cluster (use the wrapped compose project's services)")
	}
	if m.BaseDomain != "" && !domainRE.MatchString(m.BaseDomain) {
		fail("invalid base_domain %q", m.BaseDomain)
	}
	switch m.Ingress {
	case IngressNone, IngressDelegate, IngressHull:
	default:
		fail("invalid ingress %q (use %q or %q)", m.Ingress, IngressDelegate, IngressHull)
	}
	for key, rt := range m.Routes {
		if !keyRE.MatchString(key) {
			fail("invalid route key %q", key)
			continue
		}
		if rt == nil || rt.Service == "" {
			fail("route %q: 'service' is required", key)
			continue
		}
		if rt.Port < 1 || rt.Port > 65535 {
			fail("route %q: invalid port %d", key, rt.Port)
		}
		if rt.Subdomain != "" && !nameRE.MatchString(rt.Subdomain) {
			fail("route %q: invalid subdomain %q", key, rt.Subdomain)
		}
		for _, a := range rt.Aliases {
			if !nameRE.MatchString(a) {
				fail("route %q: invalid alias %q", key, a)
			}
		}
	}
	// Two routes must not resolve to the same hostname label (subdomain or
	// alias), which would make the URL ambiguous. Deterministic order.
	owner := map[string]string{}
	for _, key := range m.RouteKeys() {
		rt := m.Routes[key]
		if rt == nil {
			continue
		}
		sub := rt.Subdomain
		if sub == "" {
			sub = key
		}
		for _, label := range append([]string{sub}, rt.Aliases...) {
			if label == "" {
				continue
			}
			if prev, dup := owner[label]; dup && prev != key {
				fail("routes %q and %q both use subdomain %q", prev, key, label)
			} else {
				owner[label] = key
			}
		}
	}
}

// RouteKeys returns cluster route keys in sorted order.
func (m *Manifest) RouteKeys() []string {
	keys := make([]string, 0, len(m.Routes))
	for k := range m.Routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m *Manifest) validateServices(fail func(string, ...any)) {
	for key, s := range m.Services {
		if !keyRE.MatchString(key) {
			fail("invalid service key %q", key)
			continue
		}
		if m.Type == TypeSite && key == "app" {
			fail("service key %q collides with the site web container", key)
		}
		if _, taken := m.Containers[key]; taken {
			fail("service key %q collides with a container key", key)
		}
		if s == nil || s.Engine == "" {
			fail("service %q: 'engine' is required", key)
			continue
		}
		eng, ok := templates.Engine(s.Engine)
		if !ok {
			fail("service %q: unknown engine %q (built-ins: %s)", key, s.Engine, strings.Join(templates.EngineKeys(), ", "))
			continue
		}
		if s.Mode != ModeDedicated && s.Mode != ModeShared {
			fail("service %q: invalid mode %q (dedicated or shared)", key, s.Mode)
		}
		if !eng.IsDatabase && s.Database != "" {
			fail("service %q: 'database' is not valid for engine %q", key, s.Engine)
		}
	}
}

// hasServiceEngine reports whether any declared service uses one of the
// given engines.
func (m *Manifest) hasServiceEngine(engines ...string) bool {
	for _, s := range m.Services {
		if s == nil {
			continue
		}
		for _, e := range engines {
			if s.Engine == e {
				return true
			}
		}
	}
	return false
}

// ServiceKeys returns the service keys in deterministic (sorted) order.
func (m *Manifest) ServiceKeys() []string {
	keys := make([]string, 0, len(m.Services))
	for k := range m.Services {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ContainerKeys returns the container keys in deterministic (sorted) order.
func (m *Manifest) ContainerKeys() []string {
	keys := make([]string, 0, len(m.Containers))
	for k := range m.Containers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DatabaseService returns the key and definition of the first database
// service (sorted key order) matching one of the given engines, or ok=false.
// With no engines given, any database engine matches.
func (m *Manifest) DatabaseService(engines ...string) (string, *Service, bool) {
	for _, key := range m.ServiceKeys() {
		s := m.Services[key]
		if s == nil {
			continue
		}
		eng, ok := templates.Engine(s.Engine)
		if !ok || !eng.IsDatabase {
			continue
		}
		if len(engines) == 0 {
			return key, s, true
		}
		for _, e := range engines {
			if s.Engine == e {
				return key, s, true
			}
		}
	}
	return "", nil, false
}
