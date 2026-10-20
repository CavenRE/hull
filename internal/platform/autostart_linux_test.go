package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLingerMarkerSurvivesAndIsOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := SystemdUserUnitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(systemdUnit("/opt/hull/hull")), 0o644); err != nil {
		t.Fatal(err)
	}
	if unitMarksLinger() {
		t.Fatal("a fresh unit claims Hull enabled lingering")
	}
	markUnitLinger()
	markUnitLinger()
	data, _ := os.ReadFile(path)
	if !unitMarksLinger() || strings.Count(string(data), lingerMarker) != 1 {
		t.Errorf("unit after marking:\n%s", data)
	}
}
