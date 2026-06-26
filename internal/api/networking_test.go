package api

import (
	"testing"

	"github.com/CavenRE/hull/internal/router"
)

func TestMergeDownRoutes(t *testing.T) {
	live := []router.Route{{Domain: "up.test", Upstream: "127.0.0.1:5"}}
	all := []string{"up.test", "down.test", ""}

	got := mergeDownRoutes(live, all)

	byDomain := map[string]string{}
	for _, r := range got {
		byDomain[r.Domain] = r.Upstream
	}
	if byDomain["up.test"] != "127.0.0.1:5" {
		t.Errorf("live route changed: %q", byDomain["up.test"])
	}
	if u, ok := byDomain["down.test"]; !ok || u != "" {
		t.Errorf("down.test should be a placeholder with empty upstream, got %q (present=%v)", u, ok)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 routes (no dup, no empty domain), got %d: %+v", len(got), got)
	}
}
