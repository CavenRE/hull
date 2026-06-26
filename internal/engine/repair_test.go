package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
)

func clusterProject(home, name string) *state.Project {
	return &state.Project{
		Name: name, Dir: filepath.Join(home, name),
		Manifest: &manifest.Manifest{Schema: 1, Name: name, Type: manifest.TypeCluster, ComposeRoot: "."},
	}
}

func TestRestartForceRecreates(t *testing.T) {
	var calls [][]string
	e, home := hookEngine(t, func(_ context.Context, _, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	})
	if err := e.Restart(context.Background(), clusterProject(home, "stack")); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range calls {
		if strings.Contains(strings.Join(c, " "), "up -d --force-recreate") {
			found = true
		}
	}
	if !found {
		t.Errorf("restart did not force-recreate; calls: %v", calls)
	}
}

func TestRepairDownThenUp(t *testing.T) {
	var calls [][]string
	e, home := hookEngine(t, func(_ context.Context, _, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	})
	if err := e.Repair(context.Background(), clusterProject(home, "stack")); err != nil {
		t.Fatal(err)
	}
	downIdx, upIdx := -1, -1
	for i, c := range calls {
		j := strings.Join(c, " ")
		if downIdx == -1 && strings.Contains(j, " down") {
			downIdx = i
		}
		if upIdx == -1 && strings.Contains(j, "up -d") {
			upIdx = i
		}
	}
	if downIdx == -1 || upIdx == -1 || downIdx > upIdx {
		t.Errorf("repair should run down then up; calls: %v", calls)
	}
}
