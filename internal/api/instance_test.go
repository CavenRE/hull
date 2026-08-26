package api

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestAcquireInstanceReclaimsRecycledPID(t *testing.T) {
	orig := processLooksLikeHull
	defer func() { processLooksLikeHull = orig }()

	// A crashed daemon left a lock holding a PID the OS later recycled for an
	// unrelated (non-Hull) process: the PID is alive but is not Hull, and no
	// daemon answers. The lock must be reclaimed, not refused. Our own alive PID
	// stands in for the recycled process.
	processLooksLikeHull = func(int) bool { return false }
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, lockFileName), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := acquireInstance(home, func() bool { return false })
	if err != nil {
		t.Fatalf("a recycled-PID lock should be reclaimed, got: %v", err)
	}
	g.release()

	// Conversely, a live PID that IS a Hull process (a wedged daemon still coming
	// up) must still be refused, so two daemons never fight for the ports.
	processLooksLikeHull = func(int) bool { return true }
	home2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(home2, lockFileName), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireInstance(home2, func() bool { return false }); err == nil {
		t.Error("a live Hull-process lock should be refused, not reclaimed")
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
