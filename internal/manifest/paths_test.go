package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalPath(t *testing.T) {
	for _, ok := range []string{"", "./", ".", "public", "./web/", "a/../b"} {
		if !LocalPath(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"..", "../x", "./../../etc", "/etc", `C:\Windows`, "c:/x", `..\..\x`, "a/../../b"} {
		if LocalPath(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateRefusesEscapingContainerPaths(t *testing.T) {
	app := func(field string) string {
		return "schema: 1\nname: x\ntype: app\ncontainers:\n  web:\n    template: plain\n" + field
	}
	parseErr(t, app("    path: ../../etc\n"), "must stay inside the project folder")
	parseErr(t, "schema: 1\nname: x\ntype: app\ncontainers:\n  w:\n    build: /opt\n", "must stay inside the project folder")
	parseErr(t, app("    env:\n      A: \"1\\nB=2\"\n"), "single line")
	parseErr(t, "schema: 1\nname: x\ntemplate: plain\nenv:\n  A: \"x\\ry\"\n", "single line")
}

func TestLoadRefusesNonRegularAndHugeManifests(t *testing.T) {
	dir := t.TempDir()
	big := "schema: 1\nname: x\ntemplate: plain\n#" + strings.Repeat("x", maxManifestSize) + "\n"
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("an oversized hull.yaml was read")
	}
	linked := t.TempDir()
	target := filepath.Join(t.TempDir(), "real.yaml")
	if err := os.WriteFile(target, []byte("schema: 1\nname: x\ntemplate: plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(linked, Filename)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Load(linked); err == nil {
		t.Error("a symlinked hull.yaml was followed")
	}
}
