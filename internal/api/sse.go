package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"
)

// EventPollInterval is how often /v1/events checks the engine for changes.
// Variable so tests can shrink it.
var EventPollInterval = 2 * time.Second

// JobStreamInterval is how often /v1/jobs/{id}/stream flushes new lines.
var JobStreamInterval = 200 * time.Millisecond

// handleJobStream streams a job's log lines as SSE until it finishes.
func (s *Server) handleJobStream(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such job"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	offset := 0
	for {
		lines, running := job.LinesFrom(offset)
		for _, line := range lines {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line)
		}
		offset += len(lines)
		flusher.Flush()
		if !running {
			snap := job.Snapshot()
			payload, _ := json.Marshal(snap)
			_, _ = fmt.Fprintf(w, "event: done\ndata: %s\n\n", payload)
			flusher.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(JobStreamInterval):
		}
	}
}

// eventHub polls the running-project set once for the whole daemon and fans
// changes out to every /v1/events subscriber, so the cost is one `docker ps`
// per interval no matter how many clients (GUI panels, CLI watches) are
// connected, and nothing at all when none are. The poller starts on the first
// subscriber and stops when the last one leaves.
type eventHub struct {
	running func(context.Context) ([]string, error)

	mu      sync.Mutex
	subs    map[chan struct{}]struct{}
	current []string
	ready   bool // the poller has produced at least one snapshot
	cancel  context.CancelFunc
}

func newEventHub(running func(context.Context) ([]string, error)) *eventHub {
	return &eventHub{running: running, subs: map[chan struct{}]struct{}{}}
}

// eventHub lazily builds the shared hub the first time /v1/events is hit, so a
// Server built without NewServer (some tests) is still safe, and the poller
// reads RunningProjects at poll time (tests override it after construction).
func (s *Server) eventHub() *eventHub {
	s.eventsOnce.Do(func() {
		s.events = newEventHub(func(ctx context.Context) ([]string, error) { return s.RunningProjects(ctx) })
	})
	return s.events
}

// subscribe registers a listener and returns a signal channel (a value means
// "re-read the snapshot"), the current snapshot, whether a snapshot is ready
// yet, and an unsubscribe func.
func (h *eventHub) subscribe() (signal <-chan struct{}, snapshot []string, ready bool, unsub func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan struct{}, 1)
	h.subs[ch] = struct{}{}
	if len(h.subs) == 1 {
		var ctx context.Context
		ctx, h.cancel = context.WithCancel(context.Background())
		go h.poll(ctx)
	}
	return ch, append([]string{}, h.current...), h.ready, func() { h.unsubscribe(ch) }
}

func (h *eventHub) unsubscribe(ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; !ok {
		return
	}
	delete(h.subs, ch)
	if len(h.subs) == 0 && h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
}

func (h *eventHub) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.current...)
}

// poll refreshes the running set on a tick and signals subscribers on the first
// snapshot and on every change thereafter.
func (h *eventHub) poll(ctx context.Context) {
	for {
		if running, err := h.running(ctx); err == nil {
			h.mu.Lock()
			changed := !h.ready || !slices.Equal(running, h.current)
			h.current = running
			h.ready = true
			if changed {
				for ch := range h.subs {
					select {
					case ch <- struct{}{}:
					default: // a signal is already pending; the subscriber re-reads the latest
					}
				}
			}
			h.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(EventPollInterval):
		}
	}
}

// handleEvents pushes an SSE event whenever the set of running compose
// projects changes (and one initial snapshot on connect), driven by the shared
// hub so N connections cost one poll, not N.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	signal, snapshot, ready, unsub := s.eventHub().subscribe()
	defer unsub()

	send := func(running []string) {
		payload, _ := json.Marshal(Event{Type: "projects", Running: running})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}

	var last []string
	sentInitial := false
	if ready {
		send(snapshot)
		last, sentInitial = snapshot, true
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-signal:
			running := s.eventHub().snapshot()
			if !sentInitial {
				send(running)
				last, sentInitial = running, true
				continue
			}
			if !slices.Equal(running, last) {
				send(running)
				last = running
			}
		}
	}
}
