package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupHullHomeKeepsOlderBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, d := range []string{".hull", ".hull.bak"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".hull.bak", "keep"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	bak, err := backupHullHome()
	if err != nil {
		t.Fatal(err)
	}
	if bak != filepath.Join(home, ".hull.bak.1") {
		t.Errorf("backup went to %s", bak)
	}
	if _, err := os.Stat(filepath.Join(home, ".hull.bak", "keep")); err != nil {
		t.Error("the older backup was removed")
	}
	if _, err := os.Stat(filepath.Join(home, ".hull")); !os.IsNotExist(err) {
		t.Error("~/.hull was not moved")
	}
}
