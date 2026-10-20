// Package flock takes exclusive OS file locks. A lock holds across processes
// (the CLI and the daemon), and the OS drops it when its holder exits, so a
// crash never leaves a stale lock behind.
package flock

import (
	"errors"
	"os"
	"path/filepath"
)

var errHeld = errors.New("lock is held by another process")

// Lock blocks until it holds the lock at path (creating the file) and returns
// the release func. Locks do not nest: taking one path twice in one process
// deadlocks.
func Lock(path string) (func(), error) {
	release, _, err := acquire(path, true)
	return release, err
}

// TryLock is Lock without the wait: ok is false when another holder has it.
func TryLock(path string) (release func(), ok bool, err error) {
	return acquire(path, false)
}

// Home locks the named state lock under hullHome/locks.
func Home(hullHome, name string) (func(), error) {
	return Lock(filepath.Join(hullHome, "locks", name+".lock"))
}

func acquire(path string, wait bool) (func(), bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := lock(f, wait); err != nil {
		_ = f.Close()
		if errors.Is(err, errHeld) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = unlock(f); _ = f.Close() }, true, nil
}
