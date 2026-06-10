package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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

// handleEvents pushes an SSE event whenever the set of running compose
// projects changes (and one initial snapshot on connect).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	var last []string
	first := true
	for {
		running, err := s.RunningProjects(r.Context())
		if err == nil && (first || !slices.Equal(running, last)) {
			payload, _ := json.Marshal(Event{Type: "projects", Running: running})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			last = running
			first = false
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(EventPollInterval):
		}
	}
}
