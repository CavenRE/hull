// Package trust records the project folders the user approved, each with a
// fingerprint of the files that decide what Hull runs there, so a cloned or
// changed project never starts, runs hooks or gets routes without a yes.
package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/CavenRE/hull/internal/flock"
	"github.com/CavenRE/hull/internal/safefile"
)

const filename = "trusted.json"

type store struct {
	Projects map[string]string `json:"projects"` // Key(dir) -> fingerprint
}

// Key is the store key for a project folder: absolute and clean, and
// lowercase where the filesystem ignores case.
func Key(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		abs = strings.ToLower(abs)
	}
	return abs
}

func path(home string) string { return filepath.Join(home, filename) }

// All returns every approved folder (by Key) and its fingerprint.
func All(home string) map[string]string {
	var s store
	if data, err := os.ReadFile(path(home)); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.Projects == nil {
		s.Projects = map[string]string{}
	}
	return s.Projects
}

// Exists reports whether the store was created yet.
func Exists(home string) bool {
	_, err := os.Lstat(path(home))
	return err == nil
}

// Set records a fingerprint for each folder in entries, in one locked write.
func Set(home string, entries map[string]string) error {
	return update(home, false, func(m map[string]string) {
		for dir, fp := range entries {
			m[Key(dir)] = fp
		}
	})
}

// Remove forgets a folder.
func Remove(home, dir string) error {
	return update(home, false, func(m map[string]string) { delete(m, Key(dir)) })
}

// Seed creates the store with entries if it does not exist yet, and does
// nothing once it does.
func Seed(home string, entries map[string]string) error {
	return update(home, true, func(m map[string]string) {
		for dir, fp := range entries {
			m[Key(dir)] = fp
		}
	})
}

func update(home string, onlyIfMissing bool, fn func(map[string]string)) error {
	unlock, err := flock.Home(home, "trust")
	if err != nil {
		return err
	}
	defer unlock()
	if onlyIfMissing && Exists(home) {
		return nil
	}
	s := store{Projects: All(home)}
	fn(s.Projects)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return safefile.Write(path(home), data, 0o600)
}
