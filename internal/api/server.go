package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/jobs"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/version"
)

// Server hosts Hull's local API (hulld). Both the CLI and the GUI are
// clients of this surface — see docs/api.md.
type Server struct {
	Config *config.Config
	Engine *engine.Engine
	Jobs   *jobs.Manager
	Token  string
	// RunningProjects lists running compose projects (injectable for tests;
	// defaults to dockerx.RunningComposeProjects).
	RunningProjects func(ctx context.Context) ([]string, error)
	// OnShutdown is invoked when POST /v1/shutdown is received.
	OnShutdown func()
}

// NewServer wires a server around a config.
func NewServer(cfg *config.Config, token string) *Server {
	return &Server{
		Config:          cfg,
		Engine:          engine.New(cfg),
		Jobs:            jobs.NewManager(),
		Token:           token,
		RunningProjects: dockerx.RunningComposeProjects,
	}
}

// Handler returns the authenticated HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("GET /v1/projects", s.handleProjects)
	mux.HandleFunc("POST /v1/projects", s.handleCreateProject)
	mux.HandleFunc("POST /v1/projects/{name}/{action}", s.handleProjectAction)
	mux.HandleFunc("GET /v1/jobs", s.handleJobs)
	mux.HandleFunc("GET /v1/jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /v1/jobs/{id}/stream", s.handleJobStream)
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	mux.HandleFunc("POST /v1/shutdown", s.handleShutdown)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtleEqual(r.Header.Get("Authorization"), "Bearer "+s.Token) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, fmt.Errorf("missing or invalid token"))
	})
}

// subtleEqual compares without early exit; tokens are high-entropy so a
// length leak is acceptable.
func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, ErrorBody{Error: err.Error()})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, StatusInfo{
		Version:  version.String(),
		TLD:      s.Config.TLD,
		Roots:    s.Config.Roots,
		HullHome: s.Config.HullHome,
	})
}

// ProjectList builds the project view served by GET /v1/projects; exported
// for reuse by the CLI's in-process fallback so both paths render the same
// data.
func ProjectList(ctx context.Context, cfg *config.Config, running func(context.Context) ([]string, error)) ([]ProjectInfo, error) {
	projects, err := state.Scan(cfg.Roots)
	if err != nil {
		return nil, err
	}
	runningSet := map[string]bool{}
	if running != nil {
		if names, err := running(ctx); err == nil {
			for _, n := range names {
				runningSet[n] = true
			}
		}
	}
	infos := make([]ProjectInfo, 0, len(projects))
	for _, p := range projects {
		info := ProjectInfo{
			Name:    p.Name,
			Dir:     p.Dir,
			Running: runningSet[p.Name],
			Legacy:  p.Legacy,
		}
		switch {
		case p.Err != nil:
			info.Kind = "invalid"
			info.Error = p.Err.Error()
		case p.Legacy:
			info.Kind = "legacy"
			info.URL = "https://" + p.Name + "." + cfg.TLD
		case p.Manifest.Type == "app":
			info.Kind = "app"
		default:
			info.Kind = string(p.Manifest.Template)
			info.URL = "https://" + p.Manifest.Domain + "." + cfg.TLD
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	infos, err := ProjectList(r.Context(), s.Config, s.RunningProjects)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, infos)
}

func (s *Server) handleProjectAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	p, err := state.Find(s.Config.Roots, name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	switch action {
	case "start":
		err = s.Engine.Up(r.Context(), p)
	case "stop":
		err = s.Engine.Down(r.Context(), p)
	case "restart":
		err = s.Engine.Restart(r.Context(), p)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown action %q", action))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	job := s.Jobs.Start("create:"+req.Name, func(log func(string)) error {
		jobEngine := engine.New(s.Config)
		jobEngine.Run = captureRunner(log)
		// Jobs outlive the request that started them — background context.
		dir, err := jobEngine.NewProject(context.Background(), engine.NewOptions{
			Name:      req.Name,
			Template:  req.Template,
			DB:        req.DB,
			DBVersion: req.DBVersion,
			Redis:     req.Redis,
			PHP:       req.PHP,
			Version:   req.Version,
			SkipStart: req.SkipStart,
		})
		if err != nil {
			return err
		}
		log("created " + dir)
		return nil
	})
	writeJSON(w, http.StatusAccepted, JobRef{Job: job.Snapshot()})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Jobs.List())
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such job"))
		return
	}
	writeJSON(w, http.StatusOK, job.Snapshot())
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	if s.OnShutdown != nil {
		go s.OnShutdown()
	}
}

// captureRunner is a dockerx.Runner that records command output into a job
// log instead of the daemon's stdio. Detached from job context on purpose:
// jobs outlive the HTTP request that started them.
func captureRunner(log func(string)) dockerx.Runner {
	return func(ctx context.Context, dir, name string, args ...string) error {
		log("$ " + name + " " + strings.Join(args, " "))
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		for _, line := range strings.Split(strings.TrimRight(string(out), "\r\n"), "\n") {
			if line = strings.TrimRight(line, "\r"); line != "" {
				log(line)
			}
		}
		return err
	}
}
