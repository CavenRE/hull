package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func TestEnsureTrustedFailsClosedWithoutTerminal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cloned")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte("schema: 1\nname: cloned\ntemplate: plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{TLD: "test", Roots: []string{root}, HullHome: t.TempDir()}
	a := &app{Config: cfg, Engine: engine.New(cfg)}
	p := &state.Project{Name: "cloned", Dir: dir, Manifest: m}

	if err := a.ensureTrusted(p); err == nil || !strings.Contains(err.Error(), "hull allow cloned") {
		t.Fatalf("without a terminal the prompt must refuse and point at hull allow, got %v", err)
	}
	if a.Engine.Trusted(p) {
		t.Fatal("approved without an answer")
	}
	old := flagYes
	flagYes = true
	defer func() { flagYes = old }()
	if err := a.ensureTrusted(p); err != nil || !a.Engine.Trusted(p) {
		t.Fatalf("--yes should approve: %v", err)
	}
}
