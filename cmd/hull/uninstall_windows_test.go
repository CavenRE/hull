//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveInstalledFilesKeepsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"hull.exe", "hull.exe.old", "hull-gui.exe", "hulld.exe.new", "go.mod", filepath.Join("src", "main.go")} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	removeInstalledFiles(dir)
	for _, gone := range []string{"hull.exe", "hull.exe.old", "hull-gui.exe", "hulld.exe.new"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s was not removed", gone)
		}
	}
	for _, kept := range []string{"go.mod", filepath.Join("src", "main.go")} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}
}
