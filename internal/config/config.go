// Package config loads Hull's global settings (TLD, project roots, ports).
// Until `hull setup` exists (Phase 4), it reads ~/.hull/config.yaml when
// present and falls back to parsing a v1 ~/.hull/.env so v2 can dogfood
// side by side with a bash Hull installation.
package config

import (
	"bytes"
	"fmt"
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
	// HullHome is the resolved Hull home directory (not stored in the file).
	HullHome string `yaml:"-"`
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
// means HomeDir()). Missing files are not an error — defaults apply.
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
	if len(c.Roots) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			c.Roots = []string{filepath.Join(home, "Work", "Sites")}
		}
	}
	for i, r := range c.Roots {
		c.Roots[i] = expandPath(r)
	}
}

// Save writes config.yaml into the Hull home directory.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.HullHome, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.HullHome, Filename), data, 0o644)
}

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
