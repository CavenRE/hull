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

func TestRemoveGroup(t *testing.T) {
	s := &Store{Roots: map[string]*RootGroups{}, Members: map[string]string{}}
	root := filepath.Join("/tmp", "Sites")
	other := filepath.Join("/tmp", "Apps")
	s.AddGroup(root, "Frontend")
	s.AddGroup(root, "APIs")
	s.AddGroup(other, "Frontend") // same name, different root
	s.SetMember(filepath.Join(root, "shop"), "Frontend")
	s.SetMember(filepath.Join(root, "blog"), "Frontend")
	s.SetMember(filepath.Join(root, "api"), "APIs")
	s.SetMember(filepath.Join(other, "site"), "Frontend") // must survive

	n := s.RemoveGroup(root, "Frontend")
	if n != 2 {
		t.Errorf("ungrouped count = %d, want 2", n)
	}
	if g := s.GroupsFor(root); len(g) != 1 || g[0] != "APIs" {
		t.Errorf("GroupsFor(root) = %v, want [APIs]", g)
	}
	if s.GroupOf(filepath.Join(root, "shop")) != "" || s.GroupOf(filepath.Join(root, "blog")) != "" {
		t.Error("Frontend members under root should be ungrouped")
	}
	if s.GroupOf(filepath.Join(root, "api")) != "APIs" {
		t.Error("APIs membership must be untouched")
	}
	if s.GroupOf(filepath.Join(other, "site")) != "Frontend" {
		t.Error("same-named group in another root must not be touched")
	}

	// Removing the last group drops the root entry entirely.
	if s.RemoveGroup(root, "APIs"); s.Roots[filepath.Clean(root)] != nil {
		t.Error("empty root entry should be removed")
	}
	// No-op for a missing group.
	if n := s.RemoveGroup(root, "ghost"); n != 0 {
		t.Errorf("removing a missing group returned %d", n)
	}
}
