package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		if processAlive(pid) {
			return nil, fmt.Errorf("a previous Hull daemon (pid %d) is still running but not responding , stop it with `hull stop` or kill the process, then retry", pid)
		}
		// Dead owner: the lock is stale (a crash left it behind). Clear the
		// stale discovery file too, then try once more to take over.
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
