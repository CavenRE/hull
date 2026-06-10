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
	TypeSite Type = "site"
	TypeApp  Type = "app"
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

	Containers map[string]*Container `yaml:"containers,omitempty"` // apps only
	Services   map[string]*Service   `yaml:"services,omitempty"`
	Env        map[string]string     `yaml:"env,omitempty"`
	Hooks      Hooks                 `yaml:"hooks,omitempty"`
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
}

// Service declares infrastructure the project needs (database, cache).
// The map key becomes the compose service name and in-network hostname.
type Service struct {
	Engine   string `yaml:"engine"`
	Version  string `yaml:"version,omitempty"`
	Mode     Mode   `yaml:"mode,omitempty"`
	Database string `yaml:"database,omitempty"`
}

// Hooks are project lifecycle commands (executed from Phase 2 onward).
type Hooks struct {
	PostCreate []string `yaml:"post_create,omitempty"`
	PostImport []string `yaml:"post_import,omitempty"`
}

var (
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	keyRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	domainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	phpRE    = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

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
		fail("manifest schema %d is newer than this Hull understands (max %d) — upgrade Hull", m.Schema, CurrentSchema)
	case m.Schema < 0 || m.Schema < CurrentSchema:
		fail("unknown manifest schema %d", m.Schema)
	}

	if m.Name == "" {
		fail("'name' is required")
	} else if !nameRE.MatchString(m.Name) || len(m.Name) > 63 {
		fail("invalid name %q: lowercase letters, digits, and hyphens only, starting with a letter", m.Name)
	}

	switch m.Type {
	case TypeSite:
		m.validateSite(fail)
	case TypeApp:
		m.validateApp(fail)
	default:
		fail("invalid type %q: must be %q or %q", m.Type, TypeSite, TypeApp)
	}

	m.validateServices(fail)

	for key := range m.Env {
		if !envKeyRE.MatchString(key) {
			fail("invalid env key %q", key)
		}
	}

	return errors.Join(errs...)
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
