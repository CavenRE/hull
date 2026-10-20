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
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/envfile"
	"github.com/CavenRE/hull/internal/flock"
	"github.com/CavenRE/hull/internal/safefile"
)

// Filename is the v2 config file inside the Hull home directory.
const Filename = "config.yaml"

// Config is Hull's global configuration.
type Config struct {
	// TLD is the local top-level domain (default "test").
	TLD string `yaml:"tld"`
	// Roots are the directories scanned for projects (Sites, Apps, ...).
	Roots []string `yaml:"roots"`
	// Projects are individually-registered project directories that live
	// outside any root (imported in place with `hull import`). Each is a
	// project directory itself, loaded directly rather than scanned as a
	// parent of projects.
	Projects []string `yaml:"projects,omitempty"`
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
	// AutoReload lets the daemon watch running PHP projects and clear their
	// opcode cache when sources change. Unset means enabled.
	AutoReload *bool `yaml:"auto_reload,omitempty"`

	HullHome string `yaml:"-"`
}

// ServicesConfig controls shared-service behavior.
type ServicesConfig struct {
	// AutoAdminer auto-provisions the Adminer database console (db.<tld>) the
	// first time a database is attached to anything. Defaults to on; set to
	// false to opt out.
	AutoAdminer *bool `yaml:"auto_adminer,omitempty"`
	// Aliases maps a short name to a canonical on-disk instance name so
	// `hull services start mysql` can resolve to instance "mysql-8.4". Managed
	// with `hull services alias`. Resolved CLI-side, so a running daemon does
	// not need to be told about a freshly-added alias.
	Aliases map[string]string `yaml:"aliases,omitempty"`
	// Autostart lists shared-service instances to bring up when the daemon
	// starts (projects opt in via their own hull.yaml). Managed with
	// `hull autostart`.
	Autostart []string `yaml:"autostart,omitempty"`
	// AdminerProjectDBs attaches Adminer to the network of each project with a
	// dedicated database, so db.<tld> can reach those too. Off by default:
	// it puts one container on every project's private network.
	AdminerProjectDBs bool `yaml:"adminer_project_dbs,omitempty"`
}

// AutoAdminerEnabled reports whether Adminer should be auto-provisioned when a
// database is attached. Defaults to true when unset.
func (c *Config) AutoAdminerEnabled() bool {
	return c.Services.AutoAdminer == nil || *c.Services.AutoAdminer
}

// AutoReloadEnabled reports whether the daemon watches running PHP projects and
// clears their opcode cache when sources change. Defaults to true when unset,
// because it is what makes skipping per-request file revalidation safe. Turn it
// off (auto_reload: false) if you run your own watcher.
func (c *Config) AutoReloadEnabled() bool {
	return c.AutoReload == nil || *c.AutoReload
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

// privateHome makes the Hull home 0700 on Unix, where the mode is the access
// control: it holds the daemon token, database logins and the local CA key.
// Windows ignores the mode; the user profile's ACL covers it there.
func privateHome(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() && fi.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(dir, 0o700)
	}
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
	privateHome(hullHome)
	cfg := &Config{HullHome: hullHome}

	if data, err := os.ReadFile(filepath.Join(hullHome, Filename)); err == nil {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", Filename, err)
		}
		cfg.HullHome = hullHome
	} else if !os.IsNotExist(err) {
		// Only a missing file means defaults: an unreadable one would be
		// replaced by them on the next save.
		return nil, fmt.Errorf("reading %s: %w", Filename, err)
	} else if data, err := os.ReadFile(filepath.Join(hullHome, ".env")); err == nil {
		loadV1Env(cfg, string(data))
	}

	cfg.applyDefaults()
	// Only the label syntax here: the TLD reaches resolver files and elevated
	// scripts, but a config saved before ValidateTLD existed must still load.
	if !tldLabelRE.MatchString(cfg.TLD) {
		return nil, fmt.Errorf("%s: tld %q is not a single lowercase DNS label", Filename, cfg.TLD)
	}
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
	for i, p := range c.Projects {
		c.Projects[i] = expandPath(p)
	}
}

// Update applies fn to the config on disk while holding its OS lock, then
// saves it. Writers re-read under the lock, so the CLI and the daemon never
// save over each other's edits with a stale copy.
func Update(hullHome string, fn func(*Config) error) (*Config, error) {
	if hullHome == "" {
		hullHome = HomeDir()
	}
	unlock, err := flock.Home(hullHome, "config")
	if err != nil {
		return nil, err
	}
	defer unlock()
	c, err := Load(hullHome)
	if err != nil {
		return nil, err
	}
	if err := fn(c); err != nil {
		return nil, err
	}
	if err := c.Save(); err != nil {
		return nil, err
	}
	return c, nil
}

// Save writes config.yaml into the Hull home directory. It expands and cleans
// root paths and refuses to persist an empty roots list (keeping the daemon
// and in-process CLI paths consistent).
func (c *Config) Save() error {
	for i, r := range c.Roots {
		c.Roots[i] = expandPath(r)
	}
	for i, p := range c.Projects {
		c.Projects[i] = expandPath(p)
	}
	if len(c.Roots) == 0 && len(c.Projects) == 0 {
		return fmt.Errorf("at least one project root or registered project is required")
	}
	if err := os.MkdirAll(c.HullHome, 0o700); err != nil {
		return err
	}
	privateHome(c.HullHome)
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return safefile.Write(filepath.Join(c.HullHome, Filename), data, 0o644)
}

var tldLabelRE = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)

// publicTLDs are delegated top-level domains that a local domain must not
// shadow, because Hull would then answer for real sites. Two-letter country
// codes are refused by length. dev, app and page are also HSTS-preloaded, so
// browsers refuse plain HTTP to them anyway.
var publicTLDs = map[string]bool{
	"com": true, "net": true, "org": true, "edu": true, "gov": true, "mil": true,
	"int": true, "arpa": true, "info": true, "biz": true, "name": true, "pro": true,
	"mobi": true, "app": true, "dev": true, "page": true, "new": true, "foo": true,
	"zip": true, "mov": true, "xyz": true, "online": true, "site": true, "top": true,
	"shop": true, "store": true, "tech": true, "cloud": true, "blog": true,
	"website": true, "space": true, "live": true, "google": true,
}

// ValidateTLD accepts a local top-level domain: one lowercase DNS label that
// is not a public TLD. Every place a TLD is set from user input calls it.
func ValidateTLD(tld string) error {
	if !tldLabelRE.MatchString(tld) {
		return fmt.Errorf("invalid domain %q; use a single lowercase label like test", tld)
	}
	if len(tld) == 2 || publicTLDs[tld] {
		return fmt.Errorf("%q is a real top-level domain, so Hull would hijack real sites; use a reserved one such as test or internal", tld)
	}
	return nil
}

// localSuffixes are TLDs reserved for local or private use: they never
// resolve on the public internet.
var localSuffixes = []string{"test", "localhost", "internal", "local", "lan", "home.arpa", "example", "invalid"}

// LocalHostname reports whether host is a name Hull may serve: one under the
// configured TLD or under a reserved local TLD. A public name (a cluster
// base_domain of example.com) is refused, so the router and hosts file can
// never take over a real site.
func LocalHostname(host, tld string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, s := range append([]string{tld}, localSuffixes...) {
		if s != "" && strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// ValidLoopback reports whether s is a 127.0.0.x address with a last octet
// in 1-8, the range Hull's UI and DNS support for the router bind address.
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
