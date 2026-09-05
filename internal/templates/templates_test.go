package templates

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// The Composer-install entrypoint script must be provisioned with content:
	// it is mounted into the serversideup /etc/entrypoint.d and sourced there, so
	// a missing host file would make Docker mount an empty directory and break
	// boot. (It is sourced, not executed, so the exec bit is irrelevant, and
	// Windows does not preserve it anyway.)
	if info, err := os.Stat(filepath.Join(home, "system", "php", "hull-composer-install.sh")); err != nil {
		t.Errorf("hull-composer-install.sh not provisioned: %v", err)
	} else if info.Size() == 0 {
		t.Error("hull-composer-install.sh is empty")
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

func TestRedisInsightViewerEngine(t *testing.T) {
	e, ok := Engine("redisinsight")
	if !ok {
		t.Fatal("redisinsight engine missing from catalog")
	}
	if e.Category != "tool" {
		t.Errorf("category = %q, want tool", e.Category)
	}
	if e.UISubdomain != "redis" || e.UIPort != 5540 {
		t.Errorf("UI = %s:%d, want redis:5540", e.UISubdomain, e.UIPort)
	}
	if !e.JoinsCaddy {
		t.Error("redis viewer must join the caddy network to reach redis instances + be routed")
	}
	// Clean instance name (no version pinned in the name) like other tools.
	if got := InstanceName("redisinsight", ""); got != "redisinsight" {
		t.Errorf("InstanceName = %q, want redisinsight", got)
	}
}

// TestEnsureSystemFilesRefreshesManagedScripts locks in the delivery rule:
// Hull-owned scripts are implementation and must be refreshed when Hull ships a
// new version (otherwise a bug fix in one can never reach a machine that
// already has the old copy), while the ini files are documented as user-tunable
// and must never be clobbered.
func TestEnsureSystemFilesRefreshesManagedScripts(t *testing.T) {
	home := t.TempDir()
	if err := EnsureSystemFiles(home); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, "system", "php", "hull-fix-perms.sh")
	ini := filepath.Join(home, "system", "php", "opcache.ini")

	// Simulate an older Hull's copy of the script, and a user-tuned ini.
	if err := os.WriteFile(script, []byte("#!/bin/sh\n# stale\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	const tuned = "; my own settings\nopcache.enable=0\n"
	if err := os.WriteFile(ini, []byte(tuned), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSystemFiles(home); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "#!/bin/sh\n# stale\n" {
		t.Error("managed script was not refreshed; a fix to it could never reach existing installs")
	}
	if !strings.Contains(string(got), "HULL_WRITABLE_PATHS") {
		t.Errorf("refreshed script does not look like the shipped one: %q", string(got[:min(len(got), 80)]))
	}

	kept, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != tuned {
		t.Error("user-tuned opcache.ini was overwritten; it must be left alone")
	}
}
