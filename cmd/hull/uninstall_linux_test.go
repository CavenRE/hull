//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A single-binary install (source, AUR, release download) ships only `hull`.
// Regression guard for the bug where looksLikeHullDir required hull-gui/hulld
// and so `hull uninstall` aborted on every real Linux install.
func TestLooksLikeHullDir(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  bool
	}{
		{"single hull binary", []string{"hull"}, true},
		{"gui build", []string{"hull", "hull-gui", "hulld"}, true},
		{"daemon only", []string{"hulld"}, true},
		{"unrelated dir", []string{"ls", "notes.txt"}, false},
		{"empty dir", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := looksLikeHullDir(dir); got != tc.want {
				t.Errorf("looksLikeHullDir(%v) = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
}
