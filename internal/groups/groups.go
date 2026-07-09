// Package groups stores Hull's virtual project groups: organizational labels
// the GUI shows inside each project-root view. Membership is keyed by the
// project's absolute directory, so it works for unmanaged folders too and
// never writes into the projects themselves (decision: Hull-side, path-keyed).
package groups

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Filename is the group store inside the Hull home directory.
const Filename = "groups.yaml"

// Store is the whole group document (also the GET/PUT /v1/groups wire type).
type Store struct {
	// Roots maps a root path to its ordered group names (the order the GUI
	// renders them in).
	Roots map[string]*RootGroups `yaml:"roots,omitempty" json:"roots"`
	// Members maps a project directory to its group name.
	Members map[string]string `yaml:"members,omitempty" json:"members"`
}

// RootGroups holds one root's ordered group list.
type RootGroups struct {
	Groups []string `yaml:"groups,omitempty" json:"groups"`
}

func key(p string) string { return filepath.Clean(p) }

// Load reads the group store from hullHome; a missing file is an empty store.
func Load(hullHome string) (*Store, error) {
	s := &Store{Roots: map[string]*RootGroups{}, Members: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(hullHome, Filename))
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, err
	}
	s.ensure()
	s.normalizeKeys()
	return s, nil
}

// normalizeKeys canonicalizes root and member path keys (filepath.Clean) so
// the GUI's forward-slash paths and the daemon's OS-native paths converge to
// one key per project/root regardless of who wrote them.
func (s *Store) normalizeKeys() {
	if len(s.Roots) > 0 {
		nr := make(map[string]*RootGroups, len(s.Roots))
		for k, v := range s.Roots {
			nr[key(k)] = v
		}
		s.Roots = nr
	}
	if len(s.Members) > 0 {
		nm := make(map[string]string, len(s.Members))
		for k, v := range s.Members {
			nm[key(k)] = v
		}
		s.Members = nm
	}
}

// Save writes the store to hullHome.
func (s *Store) Save(hullHome string) error {
	s.ensure()
	s.normalizeKeys()
	if err := os.MkdirAll(hullHome, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(hullHome, Filename), data, 0o644)
}

func (s *Store) ensure() {
	if s.Roots == nil {
		s.Roots = map[string]*RootGroups{}
	}
	if s.Members == nil {
		s.Members = map[string]string{}
	}
}

// GroupOf returns a project's group label, or "" when ungrouped.
func (s *Store) GroupOf(dir string) string {
	s.ensure()
	return s.Members[key(dir)]
}

// GroupsFor returns the ordered group names defined for a root.
func (s *Store) GroupsFor(root string) []string {
	s.ensure()
	if rg := s.Roots[key(root)]; rg != nil {
		return rg.Groups
	}
	return nil
}

// AddGroup appends a group to a root (no-op if it already exists).
func (s *Store) AddGroup(root, name string) {
	s.ensure()
	k := key(root)
	rg := s.Roots[k]
	if rg == nil {
		rg = &RootGroups{}
		s.Roots[k] = rg
	}
	if !slices.Contains(rg.Groups, name) {
		rg.Groups = append(rg.Groups, name)
	}
}

// RemoveGroup deletes a group from a root and ungroups every project under
// that root that was assigned to it, returning how many projects were
// ungrouped. Removing a group that does not exist is a no-op. The projects
// themselves are never touched, grouping is Hull-side, path-keyed metadata.
func (s *Store) RemoveGroup(root, name string) int {
	s.ensure()
	k := key(root)
	if rg := s.Roots[k]; rg != nil {
		rg.Groups = slices.DeleteFunc(rg.Groups, func(g string) bool { return g == name })
		if len(rg.Groups) == 0 {
			delete(s.Roots, k) // drop the empty root entry so groups.yaml stays tidy
		}
	}
	// Ungroup members under this root that pointed at the removed group. Scoping
	// by path prevents ungrouping a same-named group in a different root.
	prefix := k + string(filepath.Separator)
	ungrouped := 0
	for dir, g := range s.Members {
		if g == name && (dir == k || strings.HasPrefix(dir, prefix)) {
			delete(s.Members, dir)
			ungrouped++
		}
	}
	return ungrouped
}

// SetMember assigns a project directory to a group (empty group = ungroup).
func (s *Store) SetMember(dir, group string) {
	s.ensure()
	if group == "" {
		delete(s.Members, key(dir))
		return
	}
	s.Members[key(dir)] = group
}

// SetOrder replaces a root's group ordering.
func (s *Store) SetOrder(root string, order []string) {
	s.ensure()
	k := key(root)
	if s.Roots[k] == nil {
		s.Roots[k] = &RootGroups{}
	}
	s.Roots[k].Groups = order
}
