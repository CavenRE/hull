// Package ledger records which projects Hull has started so they can be
// stopped reliably later , including adopted clusters whose directories live
// outside the configured roots and are therefore invisible to a roots scan.
//
// The ledger is a rebuildable index, never the only source of truth (Law 5):
// losing it only means stop-all falls back to the roots scan and the docker
// ownership-label sweep. It is keyed by compose project name.
package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/CavenRE/hull/internal/flock"
	"github.com/CavenRE/hull/internal/safefile"
)

// Entry is one started project , enough to reconstruct its compose driver.
type Entry struct {
	Name         string   `json:"name"`
	Dir          string   `json:"dir"`
	ComposeRoot  string   `json:"compose_root,omitempty"`
	ComposeFiles []string `json:"compose_files,omitempty"`
	Profiles     []string `json:"profiles,omitempty"`
	Kind         string   `json:"kind,omitempty"`
}

const filename = "started.json"

func ledgerPath(hullHome string) string { return filepath.Join(hullHome, filename) }

// List returns the recorded started projects, sorted by name. A missing or
// unreadable ledger yields an empty slice , the caller's other stop sources
// still apply.
func List(hullHome string) []Entry {
	return load(hullHome)
}

func load(hullHome string) []Entry {
	data, err := os.ReadFile(ledgerPath(hullHome))
	if err != nil {
		return nil
	}
	var entries []Entry
	if json.Unmarshal(data, &entries) != nil {
		return nil
	}
	return entries
}

func save(hullHome string, entries []Entry) error {
	if err := os.MkdirAll(hullHome, 0o755); err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return safefile.Write(ledgerPath(hullHome), data, 0o644)
}

// Add records (or updates) a started project, keyed by name.
func Add(hullHome string, e Entry) error {
	unlock, err := flock.Home(hullHome, "ledger")
	if err != nil {
		return err
	}
	defer unlock()
	entries := load(hullHome)
	for i := range entries {
		if entries[i].Name == e.Name {
			entries[i] = e
			return save(hullHome, entries)
		}
	}
	entries = append(entries, e)
	return save(hullHome, entries)
}

// Remove drops a project from the ledger (no-op when absent).
func Remove(hullHome, name string) error {
	unlock, err := flock.Home(hullHome, "ledger")
	if err != nil {
		return err
	}
	defer unlock()
	entries := load(hullHome)
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Name != name {
			out = append(out, e)
		}
	}
	return save(hullHome, out)
}
