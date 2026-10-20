// Package jobs runs long operations (scaffold, import, export) in the
// background with captured progress lines, so the CLI and GUI render the
// same feed.
package jobs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Status of a job.
type Status string

const (
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Info is a point-in-time snapshot of a job.
type Info struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Status  Status    `json:"status"`
	Error   string    `json:"error,omitempty"`
	Lines   []string  `json:"lines"`
	Created time.Time `json:"created"`
}

// Job is a running or finished operation.
type Job struct {
	id      string
	seq     int
	kind    string
	created time.Time
	cancel  context.CancelFunc

	mu     sync.Mutex
	status Status
	err    string
	lines  []string
}

func (j *Job) log(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, line)
}

func (j *Job) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err != nil {
		j.status = StatusFailed
		j.err = err.Error()
		return
	}
	j.status = StatusDone
}

func (j *Job) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status == StatusRunning
}

// Snapshot returns a copy of the job's current state.
func (j *Job) Snapshot() Info {
	j.mu.Lock()
	defer j.mu.Unlock()
	lines := make([]string, len(j.lines))
	copy(lines, j.lines)
	return Info{
		ID:      j.id,
		Kind:    j.kind,
		Status:  j.status,
		Error:   j.err,
		Lines:   lines,
		Created: j.created,
	}
}

// LinesFrom returns lines starting at offset and whether the job is still
// running , the polling primitive used by stream endpoints and the CLI.
func (j *Job) LinesFrom(offset int) (lines []string, running bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if offset < len(j.lines) {
		lines = append(lines, j.lines[offset:]...)
	}
	return lines, j.status == StatusRunning
}

// keepFinished is how many finished jobs a manager remembers; older ones are
// dropped as new jobs start, so a long-lived daemon does not grow forever.
const keepFinished = 50

// Manager owns all jobs of a daemon process.
type Manager struct {
	mu   sync.Mutex
	seq  int
	jobs map[string]*Job
	// ctx is the parent of every job's context; Shutdown cancels it.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// Now is injectable for tests.
	Now func() time.Time
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{jobs: map[string]*Job{}, Now: time.Now, ctx: ctx, cancel: cancel}
}

// Start launches fn on a goroutine. fn receives a context, canceled by Cancel
// or Shutdown, and a log callback; the job is marked done/failed from fn's
// return value.
func (m *Manager) Start(kind string, fn func(ctx context.Context, log func(string)) error) *Job {
	m.mu.Lock()
	m.seq++
	ctx, cancel := context.WithCancel(m.ctx)
	j := &Job{
		id:      fmt.Sprintf("job-%d", m.seq),
		seq:     m.seq,
		kind:    kind,
		created: m.Now(),
		status:  StatusRunning,
		cancel:  cancel,
	}
	m.jobs[j.id] = j
	m.pruneLocked()
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				j.finish(fmt.Errorf("panic: %v", r))
			}
		}()
		j.finish(fn(ctx, j.log))
	}()
	return j
}

// Cancel asks a job to stop by canceling its context; ok=false for an
// unknown id.
func (m *Manager) Cancel(id string) bool {
	j, ok := m.Get(id)
	if ok {
		j.cancel()
	}
	return ok
}

// Shutdown cancels every job and waits up to timeout for them to return, so
// a stopping daemon does not cut a job off halfway through a write.
func (m *Manager) Shutdown(timeout time.Duration) {
	m.cancel()
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// pruneLocked drops the oldest finished jobs beyond keepFinished. Caller
// holds m.mu.
func (m *Manager) pruneLocked() {
	var finished []*Job
	for _, j := range m.jobs {
		if !j.running() {
			finished = append(finished, j)
		}
	}
	if len(finished) <= keepFinished {
		return
	}
	sort.Slice(finished, func(a, b int) bool { return finished[a].seq < finished[b].seq })
	for _, j := range finished[:len(finished)-keepFinished] {
		delete(m.jobs, j.id)
	}
}

// Get returns a job by id.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// List returns snapshots of all jobs, newest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()

	infos := make([]Info, 0, len(jobs))
	for _, j := range jobs {
		infos = append(infos, j.Snapshot())
	}
	// Newest first by id sequence (ids are zero-padded-free but creation
	// order equals sequence order; sort by Created then ID for stability).
	for i := 0; i < len(infos); i++ {
		for k := i + 1; k < len(infos); k++ {
			if infos[k].Created.After(infos[i].Created) {
				infos[i], infos[k] = infos[k], infos[i]
			}
		}
	}
	return infos
}
