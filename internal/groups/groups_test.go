package groups

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	home := t.TempDir()

	// Missing file → empty store, no error.
	s, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Members) != 0 || len(s.Roots) != 0 {
		t.Fatal("fresh store should be empty")
	}

	root := filepath.Join(home, "Sites")
	s.AddGroup(root, "Frontend")
	s.AddGroup(root, "Frontend") // dedupe
	s.AddGroup(root, "APIs")
	s.SetMember(filepath.Join(root, "shop"), "Frontend")
	if err := s.Save(home); err != nil {
		t.Fatal(err)
	}

	got, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if g := got.GroupsFor(root); len(g) != 2 || g[0] != "Frontend" || g[1] != "APIs" {
		t.Errorf("GroupsFor = %v", g)
	}
	if got.GroupOf(filepath.Join(root, "shop")) != "Frontend" {
		t.Error("membership did not persist")
	}

	// Clearing membership removes it.
	got.SetMember(filepath.Join(root, "shop"), "")
	if got.GroupOf(filepath.Join(root, "shop")) != "" {
		t.Error("expected ungrouped after clear")
	}
}
