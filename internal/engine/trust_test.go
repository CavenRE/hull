package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/ledger"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func writeSite(t *testing.T, root, name, extra string) *state.Project {
	t.Helper()
	dir := filepath.Join(root, name)
	mustWrite(t, dir, "schema: 1\nname: "+name+"\ntemplate: plain\n"+extra)
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &state.Project{Name: name, Dir: dir, Manifest: m}
}

func TestUnapprovedProjectNeverRuns(t *testing.T) {
	e, root := testEngine(t) // its Run fails the test on any command
	p := writeSite(t, root, "cloned", "hooks:\n  pre_up: [\"curl evil\"]\n")
	ctx := context.Background()
	for name, run := range map[string]func() error{
		"up":      func() error { return e.Up(ctx, p) },
		"restart": func() error { return e.Restart(ctx, p) },
		"repair":  func() error { return e.Repair(ctx, p) },
		"rebuild": func() error { return e.Rebuild(ctx, p, false) },
		"reset":   func() error { return e.Reset(ctx, p) },
		"hooks":   func() error { return e.runHooks(ctx, p, "pre_up", false) },
	} {
		var ue *UntrustedError
		if err := run(); !errors.As(err, &ue) || ue.Changed {
			t.Errorf("%s: err = %v, want a not-approved UntrustedError", name, err)
		}
	}
}

func TestApprovalFollowsContent(t *testing.T) {
	e, root := testEngine(t)
	p := writeSite(t, root, "shop", "")
	approve(t, e, p)
	if !e.Trusted(p) {
		t.Fatal("not trusted right after approval")
	}
	php := "8.3"
	if err := e.SetProjectFields(p, PatchOptions{PHP: &php}); err != nil {
		t.Fatal(err)
	}
	if !e.Trusted(p) {
		t.Error("Hull's own edit voided the approval")
	}
	mustWrite(t, p.Dir, "schema: 1\nname: shop\ntemplate: plain\nhooks:\n  post_up: [\"curl evil\"]\n")
	var ue *UntrustedError
	if err := e.requireTrust(p); !errors.As(err, &ue) || !ue.Changed {
		t.Errorf("an outside edit kept the approval: %v", err)
	}
	if err := e.SetProjectFields(p, PatchOptions{PHP: &php}); err != nil {
		t.Fatal(err)
	}
	if e.Trusted(p) {
		t.Error("Hull's edit approved a project that was not approved")
	}
}

func TestStartEnabledSkipsUnapproved(t *testing.T) {
	e, root := testEngine(t)
	writeSite(t, root, "auto", "autostart: true\n")
	started, err := e.StartEnabled(context.Background())
	if started != 0 || err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Errorf("started=%d err=%v; an unapproved autostart project must not start", started, err)
	}
}

func TestRoutableAndSeed(t *testing.T) {
	e, root := testEngine(t)
	ran := writeSite(t, root, "ran", "")
	fresh := writeSite(t, root, "fresh", "")
	if err := ledger.Add(e.Config.HullHome, ledger.Entry{Name: "ran", Dir: ran.Dir}); err != nil {
		t.Fatal(err)
	}
	got := e.Routable([]state.Project{*ran, *fresh})
	if len(got) != 1 || got[0].Name != "ran" {
		t.Errorf("routable = %v; want only the project the ledger shows Hull ran before", got)
	}
	if !e.Trusted(ran) || e.Trusted(fresh) {
		t.Error("the upgrade seed must approve ledger projects only")
	}
}

func TestClusterOverrideFileNeedsApproval(t *testing.T) {
	e, root := testEngine(t)
	dir := filepath.Join(root, "stack")
	mustWrite(t, dir, "schema: 1\nname: stack\ntype: cluster\n")
	writeTestFile(t, filepath.Join(dir, "compose.yaml"), "services:\n  web:\n    image: nginx\n")
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := approve(t, e, &state.Project{Name: "stack", Dir: dir, Manifest: m})
	if got := e.composeFor(p).Files; len(got) != 1 || got[0] != "compose.yaml" {
		t.Fatalf("files = %v", got)
	}
	writeTestFile(t, filepath.Join(dir, "compose.override.yaml"), "services:\n  web:\n    privileged: true\n")
	if got := e.composeFor(p).Files; len(got) != 2 || got[1] != "compose.override.yaml" {
		t.Fatalf("override not pinned: %v", got)
	}
	if e.Trusted(p) {
		t.Error("a new override file kept the approval")
	}
}

func clusterAt(t *testing.T, root, name string, files map[string]string) *state.Project {
	t.Helper()
	dir := filepath.Join(root, name)
	mustWrite(t, dir, "schema: 1\nname: "+name+"\ntype: cluster\n")
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, path, content)
	}
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &state.Project{Name: name, Dir: dir, Manifest: m}
}

func TestEveryFileComposeReadsNeedsApproval(t *testing.T) {
	e, root := testEngine(t)
	p := clusterAt(t, root, "stack", map[string]string{
		"compose.yaml":       "include:\n  - extra/compose.yaml\nservices:\n  web:\n    image: ${IMG}\n    extends:\n      file: base.yaml\n      service: web\n",
		".env":               "IMG=nginx\n",
		"base.yaml":          "services:\n  web: {}\n",
		"extra/compose.yaml": "services:\n  worker:\n    image: ${W}\n",
		"extra/.env":         "W=busybox\n",
	})
	for _, rel := range []string{".env", "base.yaml", "extra/compose.yaml", "extra/.env"} {
		approve(t, e, p)
		path := filepath.Join(p.Dir, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, path, string(data)+"# changed\n")
		if e.Trusted(p) {
			t.Errorf("a change to %s kept the approval", rel)
		}
	}
	approve(t, e, p)
	writeTestFile(t, filepath.Join(p.Dir, "extra", "more.yaml"), "services: {}\n")
	if !e.Trusted(p) {
		t.Error("a file nothing references voided the approval")
	}
}

func TestStackPastTheLimitsCannotBeApproved(t *testing.T) {
	e, root := testEngine(t)
	big := clusterAt(t, root, "big", map[string]string{"compose.yaml": "services: {}\n#" + strings.Repeat("x", maxTrustFile)})
	if err := e.Trust(big); err == nil || e.Trusted(big) {
		t.Errorf("a compose file over the size cap was approved: %v", err)
	}
	var include strings.Builder
	include.WriteString("include:\n")
	for i := 0; i <= maxTrustRefs; i++ {
		fmt.Fprintf(&include, "  - part%d.yaml\n", i)
	}
	wide := clusterAt(t, root, "wide", map[string]string{"compose.yaml": include.String()})
	if err := e.Trust(wide); err == nil || e.Trusted(wide) {
		t.Errorf("a stack with too many included files was approved: %v", err)
	}
}

func TestSymlinkedComposeIsCoveredByItsTarget(t *testing.T) {
	e, root := testEngine(t)
	p := clusterAt(t, root, "linked", map[string]string{"real.yaml": "services:\n  web:\n    image: nginx\n"})
	if err := os.Symlink("real.yaml", filepath.Join(p.Dir, "compose.yaml")); err != nil {
		t.Skip("symbolic links are not available here:", err)
	}
	approve(t, e, p)
	writeTestFile(t, filepath.Join(p.Dir, "real.yaml"), "services:\n  web:\n    image: nginx\n    privileged: true\n")
	if e.Trusted(p) {
		t.Error("a change behind a symlinked compose file kept the approval")
	}
}

func TestComposeForPinsFiles(t *testing.T) {
	e, root := testEngine(t)
	site := writeSite(t, root, "site", "")
	if got := e.composeFor(site).Files; len(got) != 1 || got[0] != "compose.yaml" {
		t.Errorf("site files = %v", got)
	}
	dir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "docker-compose.yml"), "services: {}\n")
	writeTestFile(t, filepath.Join(dir, ".env"), "COMPOSE_FILE=a.yml"+string(os.PathListSeparator)+"b.yml\n")
	if got := e.composeFor(&state.Project{Name: "legacy", Dir: dir}).Files; strings.Join(got, ",") != "a.yml,b.yml" {
		t.Errorf("legacy files = %v, want COMPOSE_FILE honored and pinned", got)
	}
}

func TestComposeDocsReadsTheOverride(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "compose.yaml"), "services:\n  web: {}\n")
	writeTestFile(t, filepath.Join(dir, "compose.override.yaml"), "services:\n  worker: {}\n")
	if got := sortedKeys(composeServiceNames(dir, nil)); got != "web, worker" {
		t.Errorf("services = %q", got)
	}
}
