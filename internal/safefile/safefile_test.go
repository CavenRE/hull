package safefile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteReplacesAndKeepsMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	if err := Write(p, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("A=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != "A=2\n" {
		t.Errorf("content = %q", data)
	}
	if fi, _ := os.Stat(p); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want the existing 0600 kept", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(p)); len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

func TestWriteRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "compose.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Write(link, []byte("pwned"), 0o644); err == nil {
		t.Fatal("wrote through a symbolic link")
	}
	if data, _ := os.ReadFile(outside); string(data) != "keep" {
		t.Errorf("link target changed to %q", data)
	}
}
