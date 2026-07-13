package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/ledger"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanMixedRoots(t *testing.T) {
	sites := t.TempDir()
	apps := t.TempDir()
	writeFile(t, filepath.Join(sites, "alpha", "hull.yaml"), "schema: 1\nname: alpha\ntemplate: plain\n")
	writeFile(t, filepath.Join(sites, "legacy", "compose.yaml"), "services: {}\n")
	writeFile(t, filepath.Join(sites, "notes", "readme.txt"), "not a project")
	writeFile(t, filepath.Join(sites, ".hidden", "hull.yaml"), "schema: 1\nname: hidden\ntemplate: plain\n")
	writeFile(t, filepath.Join(apps, "beta", "hull.yaml"), "schema: 1\nname: beta\ntemplate: plain\n")

	projects, err := Scan([]string{sites, apps, filepath.Join(sites, "missing-root")})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range projects {
		names = append(names, p.Name)
	}
	want := "alpha,beta,legacy,notes"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("projects = %s, want %s", got, want)
	}
	for _, p := range projects {
		switch p.Name {
		case "legacy":
			if !p.Legacy {
				t.Error("legacy project not marked Legacy")
			}
		case "notes":
			if !p.Unmanaged {
				t.Error("plain folder not marked Unmanaged")
			}
		default:
			if p.Legacy || p.Unmanaged || p.Manifest == nil {
				t.Errorf("project %s: Legacy=%v Unmanaged=%v Manifest=%v", p.Name, p.Legacy, p.Unmanaged, p.Manifest)
			}
		}
	}
}

func TestScanBrokenManifestIsVisible(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "broken", "hull.yaml"), "schema: 1\nname: broken\ntemplate: nope\n")
	projects, err := Scan([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Err == nil {
		t.Fatalf("broken manifest should be listed with Err, got %+v", projects)
	}
}

func TestScanDuplicateNamesFirstWins(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeFile(t, filepath.Join(a, "dup", "hull.yaml"), "schema: 1\nname: dup\ntemplate: plain\n")
	writeFile(t, filepath.Join(b, "dup", "hull.yaml"), "schema: 1\nname: dup\ntemplate: plain\n")
	// A name collision across roots must NOT break the scan , first wins.
	projects, err := Scan([]string{a, b})
	if err != nil {
		t.Fatalf("duplicate names should not error: %v", err)
	}
	if len(projects) != 1 || projects[0].Dir != filepath.Join(a, "dup") {
		t.Errorf("expected the first root to win, got %+v", projects)
	}
}

func TestFindAndCurrent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "mysite", "hull.yaml"), "schema: 1\nname: mysite\ntemplate: plain\n")
	writeFile(t, filepath.Join(root, "mysite", "public", "index.php"), "<?php")

	p, err := Find([]string{root}, "mysite")
	if err != nil || p.Name != "mysite" {
		t.Fatalf("Find = %+v, %v", p, err)
	}
	if _, err := Find([]string{root}, "absent"); err == nil {
		t.Error("Find(absent) should error")
	}

	cur, ok := Current([]string{root}, filepath.Join(root, "mysite", "public"))
	if !ok || cur.Name != "mysite" {
		t.Errorf("Current = %+v, %v", cur, ok)
	}
	if _, ok := Current([]string{root}, t.TempDir()); ok {
		t.Error("Current outside any project should be false")
	}
}

func TestScanRegisteredProjects(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "inroot", "hull.yaml"), "schema: 1\nname: inroot\ntemplate: plain\n")
	// A project that lives OUTSIDE the root, registered individually.
	outside := filepath.Join(t.TempDir(), "creative")
	writeFile(t, filepath.Join(outside, "hull.yaml"), "schema: 1\nname: creative\ntemplate: plain\n")

	projects, err := Scan([]string{root}, outside)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range projects {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "creative,inroot" {
		t.Errorf("names = %v, want creative,inroot", names)
	}

	// Find and Current resolve the out-of-root project when it is registered.
	p, err := Find([]string{root}, "creative", outside)
	if err != nil || p.Dir != outside {
		t.Fatalf("Find(creative) = %+v, %v", p, err)
	}
	if cur, ok := Current([]string{root}, outside, outside); !ok || cur.Name != "creative" {
		t.Errorf("Current in registered dir = %+v, %v", cur, ok)
	}
	// Without the registration it is invisible.
	if _, err := Find([]string{root}, "creative"); err == nil {
		t.Error("Find should miss an unregistered out-of-root project")
	}
}

func TestScanRegisteredSelfHeals(t *testing.T) {
	root := t.TempDir()
	gone := filepath.Join(t.TempDir(), "gone") // never created
	bare := filepath.Join(t.TempDir(), "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil { // exists but has no manifest
		t.Fatal(err)
	}
	projects, err := Scan([]string{root}, gone, bare, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Errorf("stale or manifest-less registrations should be skipped, got %+v", projects)
	}
}

func TestUnder(t *testing.T) {
	root := t.TempDir()
	if !Under(root, filepath.Join(root, "app")) {
		t.Error("a child should be Under root")
	}
	if !Under(root, root) {
		t.Error("root should be Under itself")
	}
	if Under(root, t.TempDir()) {
		t.Error("an unrelated dir should not be Under root")
	}
	if Under(root, root+"app") {
		t.Error("a prefix-sharing sibling should not be Under root")
	}
}

func TestFindClusterFromLedger(t *testing.T) {
	home := t.TempDir()
	roots := []string{t.TempDir()} // deliberately does NOT contain the cluster dir
	clusterDir := filepath.Join(t.TempDir(), "outside")
	writeFile(t, filepath.Join(clusterDir, "hull.yaml"), "schema: 1\nname: orphan\ntype: cluster\ncompose_root: .\n")

	// Not reachable via a roots scan.
	if _, err := Find(roots, "orphan"); err == nil {
		t.Fatal("Find should not see an out-of-root cluster")
	}
	// Not in the ledger yet.
	if _, ok := FindCluster(home, "orphan"); ok {
		t.Fatal("FindCluster should miss before the ledger records it")
	}

	if err := ledger.Add(home, ledger.Entry{Name: "orphan", Dir: clusterDir, Kind: "cluster"}); err != nil {
		t.Fatal(err)
	}
	p, ok := FindCluster(home, "orphan")
	if !ok || p.Dir != clusterDir || p.Manifest == nil || p.Manifest.Type != "cluster" {
		t.Fatalf("FindCluster = %+v, ok=%v", p, ok)
	}
}
