package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a hub signal")
	}
}

func TestEventHubSharedPollerAndStop(t *testing.T) {
	old := EventPollInterval
	EventPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { EventPollInterval = old })

	var calls int32
	var mu sync.Mutex
	set := []string{"a"}
	running := func(ctx context.Context) ([]string, error) {
		atomic.AddInt32(&calls, 1)
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), set...), nil
	}

	h := newEventHub(running)

	// Two subscribers share one poller and both get the first snapshot.
	sig1, _, _, unsub1 := h.subscribe()
	sig2, _, _, unsub2 := h.subscribe()
	waitSignal(t, sig1)
	waitSignal(t, sig2)
	if snap := h.snapshot(); len(snap) != 1 || snap[0] != "a" {
		t.Fatalf("initial snapshot = %v, want [a]", snap)
	}

	// A change reaches both subscribers from the single poller.
	mu.Lock()
	set = []string{"a", "b"}
	mu.Unlock()
	waitSignal(t, sig1)
	waitSignal(t, sig2)
	if snap := h.snapshot(); len(snap) != 2 {
		t.Fatalf("post-change snapshot = %v, want 2 entries", snap)
	}

	// After the last subscriber leaves, the poller stops: the running-func call
	// count stops climbing (no idle polling, no leaked goroutine).
	unsub1()
	unsub2()
	settled := atomic.LoadInt32(&calls)
	time.Sleep(40 * time.Millisecond) // several poll intervals
	if grew := atomic.LoadInt32(&calls) - settled; grew > 1 {
		t.Errorf("poller kept running after the last unsubscribe (%d extra polls)", grew)
	}
}
