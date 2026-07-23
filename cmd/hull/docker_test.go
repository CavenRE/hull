package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestEveryCommandDeclaresEngineNeed is the guard that keeps this class of bug
// from coming back. Docker handling used to live in scattered per-command
// calls, which failed twice: some commands never got one, and others put theirs
// inside the in-process branch of withDaemon so the daemon-routed path ran
// unguarded. Now each command must SAY what it needs, and a new command that
// forgets fails this test instead of shipping a raw docker error to a user.
func TestEveryCommandDeclaresEngineNeed(t *testing.T) {
	applyEngineAnnotations()
	valid := map[string]bool{engineEnsure: true, engineCheck: true, engineNone: true}
	var missing, bad []string

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		// Only leaves actually run; a pure group with no RunE never executes.
		if c.RunE == nil && c.Run == nil {
			return
		}
		if c.Name() == "help" || c.Name() == "completion" || c.Parent() != nil && c.Parent().Name() == "completion" {
			return
		}
		mode, ok := c.Annotations[engineAnnotation]
		switch {
		case !ok:
			missing = append(missing, c.CommandPath())
		case !valid[mode]:
			bad = append(bad, c.CommandPath()+" = "+mode)
		}
	}
	walk(rootCmd)

	sort.Strings(missing)
	sort.Strings(bad)
	if len(missing) > 0 {
		t.Errorf("these commands do not declare an engine requirement (wrap them with needsEngine(cmd, engineEnsure|engineCheck|engineNone)):\n  %s",
			strings.Join(missing, "\n  "))
	}
	if len(bad) > 0 {
		t.Errorf("invalid engine annotation values:\n  %s", strings.Join(bad, "\n  "))
	}
}

// TestReadOnlyCommandsNeverStartDocker pins the UX decision: a command that
// reports on the engine must not launch it. `hull status` starting the thing it
// is reporting on is useless, and `hull list` in a script must not spend minutes
// booting Docker Desktop.
func TestReadOnlyCommandsNeverStartDocker(t *testing.T) {
	applyEngineAnnotations()
	readOnly := []string{
		"hull status", "hull list", "hull doctor", "hull deps",
		"hull cluster list", "hull services list",
	}
	byPath := map[string]*cobra.Command{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		byPath[c.CommandPath()] = c
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	for _, path := range readOnly {
		c, ok := byPath[path]
		if !ok {
			continue // command renamed; the annotation test still covers it
		}
		if got := c.Annotations[engineAnnotation]; got == engineEnsure {
			t.Errorf("%s is read-only and must not auto-start Docker (got %q)", path, got)
		}
	}
}
