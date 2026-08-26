package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ps "github.com/mitchellh/go-ps"
)

const lockFileName = "hulld.lock"

func lockPath(hullHome string) string { return filepath.Join(hullHome, lockFileName) }

// instanceGuard holds the single-daemon lock for the life of a daemon.
type instanceGuard struct{ path string }

func (g *instanceGuard) release() {
	if g != nil {
		_ = os.Remove(g.path)
	}
}

// acquireInstance enforces a single running daemon via a PID lock file. When a
// lock already exists it distinguishes three cases, so a second daemon can
// never silently overwrite a live one's discovery file and then die fighting
// for the shared ports (the double-hulld bug):
//
//   - a live, responding daemon            -> refuse
//   - a process alive but not responding   -> refuse with an actionable message
//     (wedged daemon, or a sibling still coming up , distinguished by PID)
//   - a dead owner (stale lock from a crash) -> clear it and take over
//
// isLive reports whether a daemon is actually answering on the recorded port.
func acquireInstance(hullHome string, isLive func() bool) (*instanceGuard, error) {
	if err := os.MkdirAll(hullHome, 0o755); err != nil {
		return nil, err
	}
	path := lockPath(hullHome)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
			_ = f.Close()
			return &instanceGuard{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		pid := readLockPID(path)
		if isLive != nil && isLive() {
			return nil, fmt.Errorf("a Hull daemon is already running (pid %d) , stop it with `hull stop` (or `hull daemon stop`)", pid)
		}
		// A live PID alone is NOT proof the daemon is up: after a crash the OS can
		// recycle the dead daemon's PID for an unrelated process (Windows reused a
		// crashed hulld's PID for a ShellHost). Only refuse when the PID is alive
		// AND the process is actually a Hull binary; otherwise the lock is stale.
		if processAlive(pid) && processLooksLikeHull(pid) {
			return nil, fmt.Errorf("a previous Hull daemon (pid %d) is still running but not responding , stop it with `hull stop` or kill the process, then retry", pid)
		}
		// Dead owner (crashed, or its PID was recycled by another process): the
		// lock is stale. Clear the stale discovery file too, then try once more to
		// take over.
		_ = os.Remove(path)
		RemoveDaemonFile(hullHome)
	}
	return nil, errors.New("could not acquire the Hull daemon lock , another hulld may be starting at the same time")
}

func readLockPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// processLooksLikeHull reports whether the process with the given PID is
// plausibly a Hull daemon, so a lock left by a crashed daemon whose PID the OS
// later recycled for something else is recognized as stale rather than mistaken
// for a live daemon. When the process cannot be identified it errs toward
// "yes", so a daemon that is only briefly unreadable (still coming up) is never
// stomped by a second one. Overridable in tests.
var processLooksLikeHull = func(pid int) bool {
	p, err := ps.FindProcess(pid)
	if err != nil || p == nil {
		return true // cannot tell: stay safe and treat it as a live daemon
	}
	return strings.Contains(strings.ToLower(p.Executable()), "hull")
}
