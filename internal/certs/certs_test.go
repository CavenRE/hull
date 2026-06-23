package certs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootCertPath(t *testing.T) {
	dir := filepath.Join("some", "caddy")
	want := filepath.Join(dir, "pki", "authorities", "local", "root.crt")
	if got := RootCertPath(dir); got != want {
		t.Errorf("RootCertPath = %q, want %q", got, want)
	}
}

func TestTrusted(t *testing.T) {
	dir := t.TempDir()
	if Trusted(dir) {
		t.Error("Trusted should be false when root.crt does not exist")
	}
	p := RootCertPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("-----BEGIN CERTIFICATE-----"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Trusted(dir) {
		t.Error("Trusted should be true once root.crt exists")
	}
}

func TestUninstallTrustNoCertIsNoop(t *testing.T) {
	// No CA on disk → nothing to remove, must not error.
	if err := UninstallTrust(t.TempDir()); err != nil {
		t.Errorf("UninstallTrust with no cert = %v, want nil", err)
	}
}
