package templates

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestInstanceName(t *testing.T) {
	if got := InstanceName("postgres", "16"); got != "postgres-16" {
		t.Errorf("InstanceName(postgres,16) = %q, want postgres-16", got)
	}
	if got := InstanceContainerName("postgres", "16"); got != "hull-postgres-16" {
		t.Errorf("InstanceContainerName = %q, want hull-postgres-16", got)
	}
	// Unknown engine falls back to the raw name.
	if got := InstanceName("nope", ""); got != "nope" {
		t.Errorf("InstanceName(nope,\"\") = %q, want nope", got)
	}
	// Empty version resolves to the engine's configured default.
	e, ok := Engine("postgres")
	if !ok {
		t.Fatal("postgres engine missing from catalog")
	}
	want := "postgres"
	if e.DefaultVersion != "" {
		want = "postgres-" + e.DefaultVersion
	}
	if got := InstanceName("postgres", ""); got != want {
		t.Errorf("InstanceName(postgres,\"\") = %q, want %q", got, want)
	}
}

func TestEngineAndSiteLookups(t *testing.T) {
	if _, ok := Engine("does-not-exist"); ok {
		t.Error("Engine returned ok for an unknown engine")
	}
	ekeys := EngineKeys()
	if len(ekeys) == 0 {
		t.Fatal("EngineKeys returned nothing")
	}
	if !sort.StringsAreSorted(ekeys) {
		t.Errorf("EngineKeys not sorted: %v", ekeys)
	}
	if !contains(ekeys, "postgres") {
		t.Errorf("EngineKeys missing postgres: %v", ekeys)
	}

	if _, ok := Site("laravel"); !ok {
		t.Error("laravel site template missing")
	}
	if _, ok := Site("does-not-exist"); ok {
		t.Error("Site returned ok for an unknown key")
	}
	skeys := SiteKeys()
	if !sort.StringsAreSorted(skeys) {
		t.Errorf("SiteKeys not sorted: %v", skeys)
	}
	for _, want := range []string{"laravel", "plain", "wordpress"} {
		if !contains(skeys, want) {
			t.Errorf("SiteKeys missing %q: %v", want, skeys)
		}
	}
}

func TestEnsureSystemFiles(t *testing.T) {
	home := t.TempDir()
	if err := EnsureSystemFiles(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "system", "php", "xdebug.ini")); err != nil {
		t.Errorf("xdebug.ini not provisioned: %v", err)
	}
	// Must be idempotent (doctor self-heals by calling it repeatedly).
	if err := EnsureSystemFiles(home); err != nil {
		t.Errorf("second EnsureSystemFiles call failed: %v", err)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
