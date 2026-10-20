package flock

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTryLockExcludesAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "x.lock")
	release, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("first TryLock: ok=%v err=%v", ok, err)
	}
	if _, ok, err := TryLock(path); err != nil || ok {
		t.Fatalf("second TryLock while held: ok=%v err=%v", ok, err)
	}
	got := make(chan struct{})
	go func() {
		r, err := Lock(path)
		if err == nil {
			r()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("Lock returned while the lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("Lock did not return after release")
	}
}
