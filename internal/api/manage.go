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
	mux.HandleFunc("DELETE /v1/projects/{name}", s.handleProjectDelete)
	mux.HandleFunc("GET /v1/logs", s.handleLogs)
	mux.HandleFunc("GET /v1/doctor", s.handleDoctor)
	mux.HandleFunc("GET /v1/detect", s.handleDetect)
}

func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	p, err := state.Find(s.Config.Roots, r.URL.Query().Get("name"), s.Config.Projects...)
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
	checks := doctor.Run(r.Context(), s.Config, doctor.Deps{
		LookPath:      exec.LookPath,
		Output:        dockerx.Output,
		DaemonVersion: version.String(),
	})
	writeJSON(w, http.StatusOK, checks)
}

func (s *Server) configInfo() ConfigInfo {
	info := ConfigInfo{TLD: s.Config.TLD, Roots: s.Config.Roots, Projects: s.Config.Projects, Loopback: s.Config.Router.Loopback}
	info.Defaults.PHP = s.Config.Defaults.PHP
	info.Defaults.Editor = s.Config.Defaults.Editor
	info.Defaults.DBTool = s.Config.Defaults.DBTool
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
	// A nil projects field (e.g. a GUI PUT that never sends it) leaves the
	// registered projects unchanged; an explicit list replaces them.
	remainingProjects := s.Config.Projects
	if req.Projects != nil {
		remainingProjects = req.Projects
	}
	if len(req.Roots) == 0 && len(remainingProjects) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("at least one root or registered project is required"))
		return
	}

	var restart []string
	if req.TLD != "" && req.TLD != s.Config.TLD {
		restart = append(restart, "tld")
		s.Config.TLD = req.TLD
	}
	if req.Loopback != "" && req.Loopback != s.Config.Router.Loopback {
		if !config.ValidLoopback(req.Loopback) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("loopback must be 127.0.0.1 to 127.0.0.8"))
			return
		}
		// The router/DNS bind address is read once at daemon start, so a
		// change only takes effect after a restart.
		restart = append(restart, "loopback")
		s.Config.Router.Loopback = req.Loopback
	}
	s.Config.Roots = req.Roots
	if req.Projects != nil {
		s.Config.Projects = req.Projects
	}
	s.Config.Defaults.PHP = req.Defaults.PHP
	s.Config.Defaults.Editor = req.Defaults.Editor
	s.Config.Defaults.DBTool = req.Defaults.DBTool

	// The GUI is v2-native: the embedded router must run (it serves every site
	// on the loopback). The legacy `enabled:false` was for side-by-side v1
	// dogfooding only , coexistence is handled by the loopback octet now. The
	// router binds at daemon start, so flipping it on needs a restart.
	if !s.Config.Router.Enabled {
		s.Config.Router.Enabled = true
		restart = append(restart, "router")
	}

	// Preserve file-only Services settings (aliases, autostart, auto_adminer)
	// that this API does not manage: reload them from disk so a config PUT
	// never clobbers a value the CLI wrote to the file after this daemon
	// started (this daemon never mutates Services in memory).
	if onDisk, err := config.Load(s.Config.HullHome); err == nil {
		s.Config.Services = onDisk.Services
	}
	if err := s.Config.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
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
	p, err := state.Find(s.Config.Roots, r.PathValue("name"), s.Config.Projects...)
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
		editor := s.Config.Defaults.Editor
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
	p, err := state.Find(s.Config.Roots, r.PathValue("name"), s.Config.Projects...)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if p.Manifest == nil {
		writeError(w, http.StatusConflict, fmt.Errorf("%s is not managed by Hull yet , import it first", p.Name))
		return
	}
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
	job := s.Jobs.Start("destroy:"+p.Name, func(log func(string)) error {
		defer s.lockProject(p.Name)()
		log("destroying " + p.Name + " (containers, volumes, files)...")
		eng := s.NewJobEngine(log)
		if err := eng.Destroy(context.Background(), p); err != nil {
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
	switch {
	case q.Get("project") != "":
		p, err := state.Find(s.Config.Roots, q.Get("project"), s.Config.Projects...)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		dir = p.Dir
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	stream := s.LogStream
	if stream == nil {
		stream = func(ctx context.Context, d string, n int, onLine func(string)) error {
			return dockerx.StreamLines(ctx, d, onLine, "docker", "compose", "logs", "--follow", "--no-color", "--tail", strconv.Itoa(n))
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
				_, _ = fmt.Fprintf(w, "data: %s\n\n", line)
			default:
				if streamErr != nil && r.Context().Err() == nil {
					log.Printf("logs: stream for %q failed: %v", dir, streamErr)
					_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", streamErr.Error())
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
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line)
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
