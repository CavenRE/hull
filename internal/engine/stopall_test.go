package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/ledger"
)

// TestStopAllStopsOutOfRootCluster is the core fix for "stop leaves things
// running": stop-all must bring down a managed project under the roots AND an
// adopted cluster recorded in the started ledger whose directory lives outside
// any configured root , the exact case the old roots-only handler missed.
func TestStopAllStopsOutOfRootCluster(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()

	// A managed site under the configured root.
	siteDir := filepath.Join(root, "site1")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "hull.yaml"),
		[]byte("schema: 1\nname: site1\ntemplate: plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// An adopted cluster living OUTSIDE the root, known only via the ledger.
	clusterDir := t.TempDir()
	if err := ledger.Add(home, ledger.Entry{Name: "extcluster", Dir: clusterDir, Kind: "cluster", ComposeRoot: "."}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{TLD: "test", Roots: []string{root}, HullHome: home}
	e := New(cfg)

	var cmds []string
	e.Run = func(ctx context.Context, dir, name string, args ...string) error {
		cmds = append(cmds, "-p "+pflag(args)+" "+lastArg(args))
		return nil
	}
	e.EnsureNet = func(ctx context.Context, name string) error { return nil }
	// No docker in tests: the label sweep returns nothing.
	e.RunningHull = func(ctx context.Context) ([]string, error) { return nil, nil }

	stopped, err := e.StopAll(context.Background())
	if err != nil {
		t.Fatalf("StopAll errored: %v", err)
	}
	if stopped != 2 {
		t.Errorf("stopped = %d, want 2 (site1 + extcluster)", stopped)
	}

	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "-p site1 down") {
		t.Errorf("did not stop the managed root project; commands:\n%s", joined)
	}
	if !strings.Contains(joined, "-p extcluster down") {
		t.Errorf("did not stop the out-of-root adopted cluster; commands:\n%s", joined)
	}

	// The cluster is removed from the ledger once stopped.
	if got := ledger.List(home); len(got) != 0 {
		t.Errorf("ledger not cleared after stop: %v", got)
	}
}

// TestStopAllStopsServicesUnconditionally is the fix for "stop leaves all
// services running": a shared instance must be brought down by stop-all even
// when the docker-ps running-detection does not report it as running (a flaky
// detection must never leak a live service).
func TestStopAllStopsServicesUnconditionally(t *testing.T) {
	home := t.TempDir()
	// A shared instance dir that exists but is NOT in the running set.
	instDir := filepath.Join(home, "services", "redis-alpine")
	if err := os.MkdirAll(instDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instDir, "compose.yaml"),
		[]byte("name: hull-redis-alpine\nservices:\n  redis:\n    image: redis\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{TLD: "test", Roots: []string{t.TempDir()}, HullHome: home}
	e := New(cfg)
	var calls []string
	e.Run = func(ctx context.Context, dir, name string, args ...string) error {
		calls = append(calls, dir+" "+strings.Join(args, " "))
		return nil
	}
	e.EnsureNet = func(ctx context.Context, name string) error { return nil }
	e.RunningHull = func(ctx context.Context) ([]string, error) { return nil, nil }

	if _, err := e.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll errored: %v", err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, instDir) || !strings.Contains(joined, "compose down") {
		t.Errorf("stop-all must down the shared instance even when not detected running; calls:\n%s", joined)
	}
}

// pflag returns the value following "-p" in a compose arg list (the project
// name), or "" when absent.
func pflag(args []string) string {
	for i, a := range args {
		if a == "-p" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}
