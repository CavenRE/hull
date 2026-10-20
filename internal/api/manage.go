package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/doctor"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/version"
)

// registerManageRoutes adds config, open, patch/delete, log, and doctor
// endpoints.
func (s *Server) registerManageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/config", s.handleConfigGet)
	mux.HandleFunc("PUT /v1/config", s.handleConfigPut)
	mux.HandleFunc("POST /v1/projects/{name}/open", s.handleProjectOpen)
	mux.HandleFunc("PATCH /v1/projects/{name}", s.handleProjectPatch)
	mux.HandleFunc("DELETE /v1/projects/{name}", s.withEngine(s.handleProjectDelete))
	mux.HandleFunc("GET /v1/logs", s.handleLogs)
	mux.HandleFunc("GET /v1/doctor", s.handleDoctor)
	mux.HandleFunc("GET /v1/detect", s.handleDetect)
}

func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	p, err := state.Find(s.cfg().Roots, r.URL.Query().Get("name"), s.cfg().Projects...)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	det := bundle.Detect(p.Dir)
	writeJSON(w, http.StatusOK, DetectInfo{
		Kind: det.Kind, Template: det.Template, PHP: det.PHP,
		DB: det.DB, Database: det.Database, Redis: det.Redis,
		Extras: det.Extras, PHPKind: det.PHPKind(),
	})
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	checks := doctor.Run(r.Context(), s.cfg(), doctor.Deps{
		LookPath:      exec.LookPath,
		Output:        dockerx.Output,
		DaemonVersion: version.String(),
	})
	writeJSON(w, http.StatusOK, checks)
}

func (s *Server) configInfo() ConfigInfo {
	info := ConfigInfo{TLD: s.cfg().TLD, Roots: s.cfg().Roots, Projects: s.cfg().Projects, Loopback: s.cfg().Router.Loopback}
	info.Defaults.PHP = s.cfg().Defaults.PHP
	info.Defaults.Editor = s.cfg().Defaults.Editor
	info.Defaults.DBTool = s.cfg().Defaults.DBTool
	return info
}

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.configInfo())
}

func (s *Server) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	var req ConfigInfo
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cur := s.cfg()
	// A nil projects field (e.g. a GUI PUT that never sends it) leaves the
	// registered projects unchanged; an explicit list replaces them.
	remainingProjects := cur.Projects
	if req.Projects != nil {
		remainingProjects = req.Projects
	}
	if len(req.Roots) == 0 && len(remainingProjects) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("at least one root or registered project is required"))
		return
	}
	if req.TLD != "" && req.TLD != cur.TLD {
		if err := config.ValidateTLD(req.TLD); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.Loopback != "" && !config.ValidLoopback(req.Loopback) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("loopback must be 127.0.0.1 to 127.0.0.8"))
		return
	}

	// Apply the request onto a fresh read of config.yaml under its lock, so the
	// settings this API does not manage (services, auto_reload) keep whatever
	// the CLI last wrote, and a concurrent writer is never saved over.
	next, err := config.Update(cur.HullHome, func(c *config.Config) error {
		if req.TLD != "" {
			c.TLD = req.TLD
		}
		if req.Loopback != "" {
			c.Router.Loopback = req.Loopback
		}
		c.Roots = req.Roots
		if req.Projects != nil {
			c.Projects = req.Projects
		}
		c.Defaults.PHP = req.Defaults.PHP
		c.Defaults.Editor = req.Defaults.Editor
		c.Defaults.DBTool = req.Defaults.DBTool
		// The GUI is v2-native: the embedded router must run (it serves every
		// site on the loopback).
		c.Router.Enabled = true
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// The DNS server, router bind address and router itself are set up once at
	// daemon start, so a change to them only takes effect after a restart.
	var restart []string
	if req.TLD != "" && req.TLD != cur.TLD {
		restart = append(restart, "tld")
	}
	if req.Loopback != "" && req.Loopback != cur.Router.Loopback {
		restart = append(restart, "loopback")
	}
	if !cur.Router.Enabled {
		restart = append(restart, "router")
	}
	s.live.Store(next)
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	resp := s.configInfo()
	resp.RestartRequired = restart
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleProjectOpen(w http.ResponseWriter, r *http.Request) {
	var req OpenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := state.Find(s.cfg().Roots, r.PathValue("name"), s.cfg().Projects...)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	switch req.Target {
	case "folder":
		name, args := fileManagerCommand(p.Dir)
		// File managers (explorer.exe notoriously) return nonzero on
		// success , fire and forget.
		_ = s.Engine.Run(r.Context(), "", name, args...)
	case "editor":
		editor := s.cfg().Defaults.Editor
		if editor == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("no editor configured , set one in Settings"))
			return
		}
		if err := s.Engine.Run(r.Context(), p.Dir, editor, p.Dir); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown target %q", req.Target))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func fileManagerCommand(dir string) (string, []string) {
	switch runtime.GOOS {
	case "windows":
		return "explorer", []string{dir}
	case "darwin":
		return "open", []string{dir}
	default:
		return "xdg-open", []string{dir}
	}
}

func (s *Server) handleProjectPatch(w http.ResponseWriter, r *http.Request) {
	var req PatchProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := state.Find(s.cfg().Roots, r.PathValue("name"), s.cfg().Projects...)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if p.Manifest == nil {
		writeError(w, http.StatusConflict, fmt.Errorf("%s is not managed by Hull yet , import it first", p.Name))
		return
	}
	defer s.lockProject(p.Name)()
	if err := s.Engine.SetProjectFields(p, engine.PatchOptions{PHP: req.PHP, Domain: req.Domain, Serve: req.Serve}); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProjectDelete(w http.ResponseWriter, r *http.Request) {
	p, err := s.findProject(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err := state.CheckUnique(s.cfg().Roots, p.Name, s.cfg().Projects...); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	job := s.Jobs.Start("destroy:"+p.Name, func(ctx context.Context, log func(string)) error {
		defer s.lockProject(p.Name)()
		log("destroying " + p.Name + " (containers, volumes, files)...")
		eng := s.NewJobEngine(log)
		if err := eng.Destroy(ctx, p); err != nil {
			return err
		}
		log("removed " + p.Dir)
		if s.SyncRoutes != nil {
			s.SyncRoutes()
		}
		return nil
	})
	writeJSON(w, http.StatusAccepted, JobRef{Job: job.Snapshot()})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tail := 200
	if t, err := strconv.Atoi(q.Get("tail")); err == nil && t > 0 && t <= 5000 {
		tail = t
	}

	var dir string
	args := []string{"compose", "logs", "--follow", "--no-color", "--tail", strconv.Itoa(tail)}
	switch {
	case q.Get("project") != "":
		p, err := s.findProject(q.Get("project"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		dir, args = s.Engine.LogsCommand(p, tail)
	case q.Get("service") != "":
		instances, err := s.Services().List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, in := range instances {
			if in.Name == q.Get("service") {
				dir = in.Dir
				break
			}
		}
		if dir == "" {
			writeError(w, http.StatusNotFound, fmt.Errorf("no shared instance %q", q.Get("service")))
			return
		}
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("pass ?project= or ?service="))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	streamOpen(w)

	stream := s.LogStream
	if stream == nil {
		stream = func(ctx context.Context, d string, _ int, onLine func(string)) error {
			return dockerx.StreamLines(ctx, d, onLine, "docker", args...)
		}
	}
	lines := make(chan string, 256)
	done := make(chan struct{})
	var streamErr error
	go func() {
		defer close(done)
		streamErr = stream(r.Context(), dir, tail, func(line string) {
			select {
			case lines <- line:
			case <-r.Context().Done():
			}
		})
	}()
	// finish drains buffered lines and, if the stream failed for a reason other
	// than the client disconnecting, emits a terminal SSE error event so the
	// client can surface it (a bare EOF otherwise reads as success).
	finish := func() {
		for {
			select {
			case line := <-lines:
				_, _ = fmt.Fprintf(w, "data: %s\n\n", sseData(line))
			default:
				if streamErr != nil && r.Context().Err() == nil {
					log.Printf("logs: stream for %q failed: %v", dir, streamErr)
					_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", sseData(streamErr.Error()))
				}
				flusher.Flush()
				return
			}
		}
	}
	flushTick := time.NewTicker(150 * time.Millisecond)
	defer flushTick.Stop()
	dirty := false
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", sseData(line))
			dirty = true
		case <-flushTick.C:
			if dirty {
				flusher.Flush()
				dirty = false
			}
		case <-done:
			finish()
			return
		case <-r.Context().Done():
			return
		}
	}
}
