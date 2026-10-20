package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

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

func TestWatchEngineFoldsABurstAndRetries(t *testing.T) {
	oldD, oldR := engineEventDebounce, engineEventRetry
	engineEventDebounce, engineEventRetry = 20*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { engineEventDebounce, engineEventRetry = oldD, oldR })

	var attempts, changes atomic.Int32
	events := func(ctx context.Context, onEvent func()) error {
		if attempts.Add(1) == 1 {
			return errors.New("stream broke")
		}
		for i := 0; i < 5; i++ {
			onEvent()
		}
		<-ctx.Done()
		return nil
	}
	stop := watchEngine(context.Background(), events, func() { changes.Add(1) }, func(string, ...any) {})
	for deadline := time.Now().Add(2 * time.Second); changes.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	stop()
	if n := changes.Load(); n != 1 {
		t.Errorf("a burst of 5 events made %d reconciles, want 1", n)
	}
	if n := attempts.Load(); n != 2 {
		t.Errorf("stream attempts = %d, want 2 (one retry)", n)
	}
}
