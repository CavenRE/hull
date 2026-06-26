package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireInstanceFreshAndRelease(t *testing.T) {
	home := t.TempDir()
	g, err := acquireInstance(home, func() bool { return false })
	if err != nil {
		t.Fatalf("fresh acquire failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, lockFileName)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}
	g.release()
	if _, err := os.Stat(filepath.Join(home, lockFileName)); !os.IsNotExist(err) {
		t.Error("lock file not removed on release")
	}
}

func TestAcquireInstanceRefusesWhenLive(t *testing.T) {
	home := t.TempDir()
	g, err := acquireInstance(home, func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	defer g.release()
	// A second daemon must be refused while one is present (live, or , as here ,
	// our own still-alive PID holds the lock).
	if _, err := acquireInstance(home, func() bool { return true }); err == nil {
		t.Error("second acquire should be refused when a daemon is present")
	}
}

func TestAcquireInstanceTakesOverStaleLock(t *testing.T) {
	home := t.TempDir()
	// A stale lock from a crashed daemon: a PID that does not exist and no live
	// daemon answering.
	if err := os.WriteFile(filepath.Join(home, lockFileName), []byte("2000000000"), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := acquireInstance(home, func() bool { return false })
	if err != nil {
		t.Fatalf("should take over a stale lock, got: %v", err)
	}
	defer g.release()
	if readLockPID(filepath.Join(home, lockFileName)) != os.Getpid() {
		t.Error("stale lock not rewritten with our pid")
	}
}

func TestProcessAliveSelfAndDead(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("processAlive(self) = false, want true")
	}
	if processAlive(2000000000) {
		t.Error("processAlive(nonexistent) = true, want false")
	}
}
