package api

import (
	"testing"
)

func TestAcquireInstanceIsExclusiveAndReleases(t *testing.T) {
	home := t.TempDir()
	g, err := acquireInstance(home, func() bool { return false })
	if err != nil {
		t.Fatalf("fresh acquire failed: %v", err)
	}
	if _, err := acquireInstance(home, func() bool { return false }); err == nil {
		t.Error("a second daemon got the lock while the first held it")
	}
	g.release()
	g2, err := acquireInstance(home, func() bool { return false })
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	g2.release()
}

func TestAcquireInstanceRefusesAnswering(t *testing.T) {
	if _, err := acquireInstance(t.TempDir(), func() bool { return true }); err == nil {
		t.Error("acquired although a daemon answers on the recorded port")
	}
}
