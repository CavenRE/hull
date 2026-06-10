package jobs

import (
	"errors"
	"testing"
	"time"
)

func wait(t *testing.T, j *Job) Info {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := j.Snapshot(); s.Status != StatusRunning {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Info{}
}

func TestJobSuccess(t *testing.T) {
	m := NewManager()
	j := m.Start("test", func(log func(string)) error {
		log("step 1")
		log("step 2")
		return nil
	})
	s := wait(t, j)
	if s.Status != StatusDone || len(s.Lines) != 2 || s.Lines[1] != "step 2" {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestJobFailure(t *testing.T) {
	m := NewManager()
	j := m.Start("test", func(log func(string)) error {
		return errors.New("boom")
	})
	s := wait(t, j)
	if s.Status != StatusFailed || s.Error != "boom" {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestJobPanicIsFailure(t *testing.T) {
	m := NewManager()
	j := m.Start("test", func(log func(string)) error {
		panic("kaboom")
	})
	s := wait(t, j)
	if s.Status != StatusFailed {
		t.Errorf("status = %s, want failed", s.Status)
	}
}

func TestLinesFrom(t *testing.T) {
	m := NewManager()
	done := make(chan struct{})
	j := m.Start("test", func(log func(string)) error {
		log("a")
		log("b")
		<-done
		log("c")
		return nil
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if lines, _ := j.LinesFrom(0); len(lines) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for first lines")
		}
		time.Sleep(5 * time.Millisecond)
	}
	lines, running := j.LinesFrom(1)
	if !running || len(lines) != 1 || lines[0] != "b" {
		t.Errorf("LinesFrom(1) = %v, running=%v", lines, running)
	}
	close(done)
	s := wait(t, j)
	if len(s.Lines) != 3 {
		t.Errorf("lines = %v", s.Lines)
	}
}

func TestGetAndList(t *testing.T) {
	m := NewManager()
	j := m.Start("alpha", func(log func(string)) error { return nil })
	wait(t, j)
	if _, ok := m.Get(j.Snapshot().ID); !ok {
		t.Error("Get(existing) = false")
	}
	if _, ok := m.Get("job-999"); ok {
		t.Error("Get(missing) = true")
	}
	if got := len(m.List()); got != 1 {
		t.Errorf("List len = %d", got)
	}
}
