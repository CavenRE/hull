package api

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/CavenRE/hull/internal/flock"
)

// instanceGuard holds the single-daemon lock for the life of a daemon.
type instanceGuard struct{ unlock func() }

func (g *instanceGuard) release() {
	if g != nil && g.unlock != nil {
		g.unlock()
	}
}

// acquireInstance enforces one daemon per Hull home with an OS lock. The OS
// drops the lock when its holder exits, so a crash never leaves a stale lock
// and a recycled PID can never be mistaken for a live daemon. isLive reports
// whether a daemon answers on the recorded port; it still refuses one from an
// older Hull that predates the lock.
func acquireInstance(hullHome string, isLive func() bool) (*instanceGuard, error) {
	unlock, ok, err := flock.TryLock(filepath.Join(hullHome, "locks", "daemon.lock"))
	if err != nil {
		return nil, err
	}
	if !ok {
		if isLive != nil && isLive() {
			return nil, errors.New("a Hull daemon is already running , stop it with `hull stop` (or `hull daemon stop`)")
		}
		return nil, errors.New("another Hull daemon holds the lock but is not responding , stop it with `hull stop` or end that hull process, then retry")
	}
	if isLive != nil && isLive() {
		unlock()
		return nil, errors.New("a Hull daemon is already running , stop it with `hull stop` (or `hull daemon stop`)")
	}
	_ = os.Remove(filepath.Join(hullHome, "hulld.lock")) // PID lock file of older versions
	return &instanceGuard{unlock: unlock}, nil
}
