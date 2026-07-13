package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsWhenEmpty(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLD != "test" {
		t.Errorf("tld = %q, want test", cfg.TLD)
	}
	if len(cfg.Roots) == 0 {
		t.Error("expected a default root")
	}
}

func TestLoadConfigYAML(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	content := "tld: dev\nroots:\n  - " + root + "\n"
	if err := os.WriteFile(filepath.Join(home, Filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLD != "dev" {
		t.Errorf("tld = %q, want dev", cfg.TLD)
	}
	if len(cfg.Roots) != 1 || cfg.Roots[0] != filepath.Clean(root) {
		t.Errorf("roots = %v, want [%s]", cfg.Roots, root)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, Filename), []byte("bogus: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected unknown-field error, got %v", err)
	}
}

func TestLoadV1EnvFallback(t *testing.T) {
	home := t.TempDir()
	env := "SITES_DIR=$HOME/Work/Sites\nTLD=.local\nHTTP_PORT=80\n"
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLD != "local" {
		t.Errorf("tld = %q, want local (leading dot stripped)", cfg.TLD)
	}
	userHome, _ := os.UserHomeDir()
	want := filepath.Join(userHome, "Work", "Sites")
	if len(cfg.Roots) != 1 || cfg.Roots[0] != want {
		t.Errorf("roots = %v, want [%s]", cfg.Roots, want)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	home := t.TempDir()
	cfg := &Config{TLD: "dev", Roots: []string{t.TempDir()}, HullHome: home}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TLD != cfg.TLD || len(loaded.Roots) != 1 || loaded.Roots[0] != cfg.Roots[0] {
		t.Errorf("round trip mismatch: %+v vs %+v", loaded, cfg)
	}
}

func TestProjectsRoundTrip(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	cfg := &Config{TLD: "dev", Roots: []string{t.TempDir()}, Projects: []string{proj}, HullHome: home}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != 1 || loaded.Projects[0] != filepath.Clean(proj) {
		t.Errorf("projects round trip = %v, want [%s]", loaded.Projects, proj)
	}
}

func TestSaveAllowsProjectsOnlyAndRejectsEmpty(t *testing.T) {
	// A projects-only config (no parked roots) is valid.
	only := &Config{Projects: []string{t.TempDir()}, HullHome: t.TempDir()}
	if err := only.Save(); err != nil {
		t.Errorf("projects-only config should save, got %v", err)
	}
	// Neither roots nor projects is rejected.
	empty := &Config{HullHome: t.TempDir()}
	if err := empty.Save(); err == nil {
		t.Error("a config with no roots and no projects should be rejected")
	}
}

func TestValidLoopback(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "127.0.0.3", "127.0.0.8"} {
		if !ValidLoopback(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "127.0.0.0", "127.0.0.9", "127.0.1.1", "10.0.0.1", "::1", "nonsense"} {
		if ValidLoopback(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestLoopbackDefaultsTo2(t *testing.T) {
	// Hull defaults to 127.0.0.2 so it owns its own loopback IP and never
	// fights another local service for :80/:443/:53 on 127.0.0.1.
	c := &Config{HullHome: t.TempDir(), Router: RouterConfig{Loopback: "bogus"}}
	c.applyDefaults()
	if c.Router.Loopback != "127.0.0.2" {
		t.Errorf("expected default 127.0.0.2, got %q", c.Router.Loopback)
	}
}
