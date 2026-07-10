// Package config loads Hull's global settings (TLD, project roots, ports).
// Until `hull setup` exists (Phase 4), it reads ~/.hull/config.yaml when
// present and falls back to parsing a v1 ~/.hull/.env so v2 can dogfood
// side by side with a bash Hull installation.
package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/envfile"
)

// Filename is the v2 config file inside the Hull home directory.
const Filename = "config.yaml"

// Config is Hull's global configuration.
type Config struct {
	// TLD is the local top-level domain (default "test").
	TLD string `yaml:"tld"`
	// Roots are the directories scanned for projects (Sites, Apps, ...).
	Roots []string `yaml:"roots"`
	// Router configures the embedded HTTPS router (Phase 4); disabled
	// means the v1 caddy-docker-proxy stack keeps routing.
	Router RouterConfig `yaml:"router,omitempty"`
	// DNS configures the embedded wildcard resolver.
	DNS DNSConfig `yaml:"dns,omitempty"`
	// Services configures shared-service behavior.
	Services ServicesConfig `yaml:"services,omitempty"`
	// Defaults are user preferences applied to new things.
	Defaults Defaults `yaml:"defaults,omitempty"`
	// HullHome is the resolved Hull home directory (not stored in the file).
	HullHome string `yaml:"-"`
}

// ServicesConfig controls shared-service behavior.
type ServicesConfig struct {
	// AutoAdminer auto-provisions the Adminer database console (db.<tld>) the
	// first time a database is attached to anything. Defaults to on; set to
	// false to opt out.
	AutoAdminer *bool `yaml:"auto_adminer,omitempty"`
}

// AutoAdminerEnabled reports whether Adminer should be auto-provisioned when a
// database is attached. Defaults to true when unset.
func (c *Config) AutoAdminerEnabled() bool {
	return c.Services.AutoAdminer == nil || *c.Services.AutoAdminer
}

// Defaults are user preferences (Settings page).
type Defaults struct {
	// PHP version for new sites (empty = templates.DefaultPHP).
	PHP string `yaml:"php,omitempty"`
	// Editor command for "Open in editor" (e.g. "code").
	Editor string `yaml:"editor,omitempty"`
	// DBTool for "Open with" on database instances:
	// tableplus | adminer | none.
	DBTool string `yaml:"db_tool,omitempty"`
}

// RouterConfig controls the embedded Caddy router run by hulld.
type RouterConfig struct {
	Enabled   bool `yaml:"enabled"`
	HTTPPort  int  `yaml:"http_port,omitempty"`
	HTTPSPort int  `yaml:"https_port,omitempty"`
	// Loopback is the 127.0.0.0/8 address the router and embedded DNS bind to
	// and resolve *.tld to. Defaults to 127.0.0.2 so Hull owns its own loopback
	// IP and never fights another local service for :80/:443/:53 on 127.0.0.1;
	// any 127.0.0.x last octet works. On macOS a non-.1 address needs a lo0
	// alias, which `hull setup` adds. Everything on Hull's side (router bind,
	// DNS bind, DNS answer, and the OS DNS registration) uses this address.
	Loopback string `yaml:"loopback,omitempty"`
}

// DNSConfig controls the embedded wildcard DNS server run by hulld.
type DNSConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port,omitempty"`
}

// RouterDataDir is where the embedded router stores its CA and certs.
func (c *Config) RouterDataDir() string {
	return filepath.Join(c.HullHome, "caddy")
}

// HomeDir resolves the Hull home directory: $HULL_HOME or ~/.hull.
func HomeDir() string {
	if h := os.Getenv("HULL_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".hull"
	}
	return filepath.Join(home, ".hull")
}

// Load reads the configuration for the given Hull home directory (empty
// means HomeDir()). Missing files are not an error , defaults apply.
func Load(hullHome string) (*Config, error) {
	if hullHome == "" {
		hullHome = HomeDir()
	}
	cfg := &Config{HullHome: hullHome}

	if data, err := os.ReadFile(filepath.Join(hullHome, Filename)); err == nil {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", Filename, err)
		}
		cfg.HullHome = hullHome
	} else if data, err := os.ReadFile(filepath.Join(hullHome, ".env")); err == nil {
		loadV1Env(cfg, string(data))
	}

	cfg.applyDefaults()
	return cfg, nil
}

// loadV1Env maps a bash Hull .env (SITES_DIR, TLD) onto the config.
func loadV1Env(cfg *Config, content string) {
	if v, ok := envfile.Get(content, "SITES_DIR"); ok {
		if dir := expandPath(v); dir != "" {
			cfg.Roots = []string{dir}
		}
	}
	if v, ok := envfile.Get(content, "TLD"); ok {
		cfg.TLD = strings.TrimPrefix(strings.TrimSpace(v), ".")
	}
}

func (c *Config) applyDefaults() {
	if c.TLD == "" {
		c.TLD = "test"
	}
	if c.Router.HTTPPort == 0 {
		c.Router.HTTPPort = 80
	}
	if c.Router.HTTPSPort == 0 {
		c.Router.HTTPSPort = 443
	}
	if c.DNS.Port == 0 {
		c.DNS.Port = 53
	}
	if !ValidLoopback(c.Router.Loopback) {
		c.Router.Loopback = "127.0.0.2"
	}
	if len(c.Roots) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			c.Roots = []string{filepath.Join(home, "Work", "Sites")}
		}
	}
	for i, r := range c.Roots {
		c.Roots[i] = expandPath(r)
	}
}

// Save writes config.yaml into the Hull home directory. It expands and cleans
// root paths and refuses to persist an empty roots list (keeping the daemon
// and in-process CLI paths consistent).
func (c *Config) Save() error {
	for i, r := range c.Roots {
		c.Roots[i] = expandPath(r)
	}
	if len(c.Roots) == 0 {
		return fmt.Errorf("at least one project root is required")
	}
	if err := os.MkdirAll(c.HullHome, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.HullHome, Filename), data, 0o644)
}

// ValidLoopback reports whether s is a 127.0.0.x address with a last octet
// in 1–8 , the range Hull's UI and DNS support for the router bind address.
func ValidLoopback(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	v4 := ip.To4()
	if v4 == nil || v4[0] != 127 || v4[1] != 0 || v4[2] != 0 {
		return false
	}
	return v4[3] >= 1 && v4[3] <= 8
}

// ExpandPath expands $VARS, a leading ~, and cleans a path , exported so the
// API/CLI can normalize roots before persisting.
func ExpandPath(p string) string { return expandPath(p) }

// expandPath expands $VARS, a leading ~, and cleans the result. v1 .env
// files contain literal "$HOME/Work/Sites".
func expandPath(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"'`)
	p = os.Expand(p, func(name string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		// v1 .env files reference $HOME, which Windows does not set.
		if name == "HOME" {
			if home, err := os.UserHomeDir(); err == nil {
				return home
			}
		}
		return ""
	})
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimLeft(p[1:], `/\`))
		}
	}
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}
