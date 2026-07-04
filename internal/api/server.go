package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/groups"
	"github.com/CavenRE/hull/internal/jobs"
	"github.com/CavenRE/hull/internal/ledger"
	"github.com/CavenRE/hull/internal/registry"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
	"github.com/CavenRE/hull/internal/version"
)

// Server hosts Hull's local API (hulld). Both the CLI and the GUI are
// clients of this surface , see docs/api.md.
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
	// SyncRoutes reconciles the embedded router after lifecycle changes
	// (no-op when networking is disabled).
	SyncRoutes func()
	// NewJobEngine builds the engine a background job uses, with command
	// output captured into the job log. Injectable for tests.
	NewJobEngine func(log func(string)) *engine.Engine
	// Services returns the shared-services manager. Injectable for tests.
	Services func() *services.Manager
	// JobServices returns a manager whose command output lands in a job
	// log. Injectable for tests.
	JobServices func(log func(string)) *services.Manager
	// LogStream follows container logs for a compose dir (injectable for
	// tests; nil = docker compose logs --follow).
	LogStream func(ctx context.Context, dir string, tail int, onLine func(string)) error
	// Registry queries Docker Hub for live image versions and search.
	Registry *registry.Client
	// projectLocks serializes mutating operations on the same project so two
	// concurrent jobs (e.g. a GUI restart and a CLI reset) cannot interleave.
	projectLocks *keyedMutex
}

// keyedMutex hands out a mutex per key (project name), so operations on
// different projects stay concurrent while same-project ones serialize.
type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

func newKeyedMutex() *keyedMutex { return &keyedMutex{m: map[string]*sync.Mutex{}} }

func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	mu, ok := k.m[key]
	if !ok {
		mu = &sync.Mutex{}
		k.m[key] = mu
	}
	k.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// findProject resolves a project by name, falling back to a ledger-known
// cluster whose directory is outside the configured roots, so both the CLI and
// the daemon resolve out-of-root clusters the same way.
func (s *Server) findProject(name string) (*state.Project, error) {
	p, err := state.Find(s.Config.Roots, name)
	if err == nil {
		return p, nil
	}
	if lp, ok := state.FindCluster(s.Config.HullHome, name); ok {
		return lp, nil
	}
	return nil, err
}

// lockProject acquires the per-project lock and returns its release func. It is
// nil-safe: a Server built without NewServer (some tests) does no locking.
func (s *Server) lockProject(name string) func() {
	if s.projectLocks == nil {
		return func() {}
	}
	return s.projectLocks.lock(name)
}

// NewServer wires a server around a config.
func NewServer(cfg *config.Config, token string) *Server {
	return &Server{
		Config:          cfg,
		Engine:          engine.New(cfg),
		Jobs:            jobs.NewManager(),
		Token:           token,
		RunningProjects: dockerx.RunningComposeProjects,
		NewJobEngine: func(log func(string)) *engine.Engine {
			e := engine.New(cfg)
			e.Run = captureRunner(log)
			return e
		},
		Services: func() *services.Manager {
			return services.NewManager(cfg)
		},
		JobServices: func(log func(string)) *services.Manager {
			m := services.NewManager(cfg)
			m.Run = captureRunner(log)
			return m
		},
		Registry:     registry.New(),
		projectLocks: newKeyedMutex(),
	}
}

// Handler returns the authenticated HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("GET /v1/projects", s.handleProjects)
	mux.HandleFunc("POST /v1/projects", s.handleCreateProject)
	mux.HandleFunc("GET /v1/projects/{name}/volumes", s.handleProjectVolumes)
	mux.HandleFunc("POST /v1/projects/{name}/{action}", s.handleProjectAction)
	mux.HandleFunc("POST /v1/imports", s.handleImport)
	mux.HandleFunc("GET /v1/clusters", s.handleClusters)
	mux.HandleFunc("POST /v1/clusters", s.handleAdoptCluster)
	mux.HandleFunc("POST /v1/clusters/create", s.handleCreateCluster)
	mux.HandleFunc("PUT /v1/clusters/{name}", s.handleClusterConfigSet)
	mux.HandleFunc("PUT /v1/clusters/{name}/routes/{key}", s.handleClusterRouteSet)
	mux.HandleFunc("DELETE /v1/clusters/{name}/routes/{key}", s.handleClusterRouteDelete)
	s.registerServiceRoutes(mux)
	s.registerManageRoutes(mux)
	s.registerSetupRoutes(mux)
	s.registerRegistryRoutes(mux)
	s.registerGroupRoutes(mux)
	s.registerDependencyRoutes(mux)
	mux.HandleFunc("GET /v1/jobs", s.handleJobs)
	mux.HandleFunc("GET /v1/jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /v1/jobs/{id}/stream", s.handleJobStream)
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	mux.HandleFunc("POST /v1/stop-all", s.handleStopAll)
	mux.HandleFunc("POST /v1/shutdown", s.handleShutdown)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS for the GUI webview (tauri://localhost etc.). Safe because
		// the bearer token , not the origin , is the access control, and
		// the server only listens on loopback.
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		token := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if q := r.URL.Query().Get("token"); q != "" {
			// EventSource cannot set headers; SSE clients pass ?token=.
			token = q
		}
		if subtleEqual(token, s.Token) {
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
	grp, _ := groups.Load(cfg.HullHome) // best-effort; nil-safe below
	infos := make([]ProjectInfo, 0, len(projects))
	for _, p := range projects {
		info := ProjectInfo{
			Name:    p.Name,
			Dir:     p.Dir,
			Running: runningSet[p.Name],
			Legacy:  p.Legacy,
		}
		if grp != nil {
			info.Group = grp.GroupOf(p.Dir)
		}
		switch {
		case p.Err != nil:
			info.Kind = "invalid"
			info.Error = p.Err.Error()
		case p.Unmanaged:
			info.Kind = "folder" // import candidate; docker never touched
		case p.Legacy:
			info.Kind = "legacy"
			info.URL = "https://" + p.Name + "." + cfg.TLD
			info.Served = true
		case p.Manifest.Type == "app":
			info.Kind = "app"
		case p.Manifest.Type == "cluster":
			info.Kind = "cluster"
			info.Served = true
		default:
			info.Kind = string(p.Manifest.Template)
			if p.Manifest.Served() {
				info.URL = "https://" + p.Manifest.Domain + "." + cfg.TLD
			}
		}
		if m := p.Manifest; m != nil && m.Type == "cluster" {
			suffix := m.ClusterSuffix(cfg.TLD)
			for _, k := range m.RouteKeys() {
				rt := m.Routes[k]
				info.Routes = append(info.Routes, ClusterRouteInfo{
					Key: k, Subdomain: rt.Subdomain, Service: rt.Service, Port: rt.Port,
					Served: rt.Served(), Aliases: rt.Aliases, Hosts: rt.Hosts(suffix),
				})
			}
		}
		if m := p.Manifest; m != nil {
			info.PHP = m.PHP
			info.Served = m.Served()
			for _, key := range m.ServiceKeys() {
				svc := m.Services[key]
				link := ProjectServiceInfo{
					Key: key, Engine: svc.Engine, Version: svc.Version, Mode: string(svc.Mode),
				}
				if string(svc.Mode) == "shared" {
					link.Instance = templates.InstanceName(svc.Engine, svc.Version)
				}
				info.Services = append(info.Services, link)
			}
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

// ClusterList returns adopted/managed clusters (type: cluster), reconciled
// with the started ledger so adopted clusters whose directories live outside
// the configured roots are still listed. Exported so the CLI's in-process
// fallback renders the same data as the daemon (core-first).
func ClusterList(ctx context.Context, cfg *config.Config, running func(context.Context) ([]string, error)) ([]ClusterInfo, error) {
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
	seen := map[string]bool{}
	out := []ClusterInfo{}
	for i := range projects {
		m := projects[i].Manifest
		if m == nil || m.Type != "cluster" {
			continue
		}
		suffix := m.ClusterSuffix(cfg.TLD)
		ci := ClusterInfo{
			Name: m.Name, Dir: projects[i].Dir, ComposeRoot: m.ComposeRoot,
			Running: runningSet[m.Name], BaseDomain: m.BaseDomain, Ingress: m.Ingress,
		}
		for _, k := range m.RouteKeys() {
			rt := m.Routes[k]
			ci.Routes = append(ci.Routes, ClusterRouteInfo{
				Key: k, Subdomain: rt.Subdomain, Service: rt.Service, Port: rt.Port,
				Served: rt.Served(), Aliases: rt.Aliases, Hosts: rt.Hosts(suffix),
			})
		}
		out = append(out, ci)
		seen[m.Name] = true
	}
	// Reconcile out-of-root adopted clusters recorded in the started ledger.
	for _, e := range ledger.List(cfg.HullHome) {
		if e.Kind != "cluster" || seen[e.Name] {
			continue
		}
		out = append(out, ClusterInfo{Name: e.Name, Dir: e.Dir, ComposeRoot: e.ComposeRoot, Running: runningSet[e.Name]})
		seen[e.Name] = true
	}
	return out, nil
}

func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
	list, err := ClusterList(r.Context(), s.Config, s.RunningProjects)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleClusterConfigSet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req SetClusterConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.findProject(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer s.lockProject(name)()
	if err := s.Engine.SetClusterConfig(p, engine.ClusterConfigSpec{BaseDomain: req.BaseDomain, Ingress: req.Ingress}); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClusterRouteSet(w http.ResponseWriter, r *http.Request) {
	name, key := r.PathValue("name"), r.PathValue("key")
	var req SetRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.findProject(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer s.lockProject(name)()
	if err := s.Engine.SetClusterRoute(p, key, engine.ClusterRouteSpec{
		Service: req.Service, Port: req.Port, Aliases: req.Aliases, Serve: req.Serve,
	}); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClusterRouteDelete(w http.ResponseWriter, r *http.Request) {
	name, key := r.PathValue("name"), r.PathValue("key")
	p, err := s.findProject(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer s.lockProject(name)()
	if err := s.Engine.RemoveClusterRoute(p, key); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProjectAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	p, err := s.findProject(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	// rebuild/reset can be slow (image builds, volume teardown) , run them as
	// background jobs with streamed logs, like create/destroy.
	switch action {
	case "rebuild", "reset":
		noCache := r.URL.Query().Get("no_cache") != ""
		job := s.Jobs.Start(action+":"+p.Name, func(log func(string)) error {
			defer s.lockProject(p.Name)()
			eng := s.NewJobEngine(log)
			var jerr error
			if action == "rebuild" {
				log("rebuilding " + p.Name + (map[bool]string{true: " (no cache)"}[noCache]))
				jerr = eng.Rebuild(context.Background(), p, noCache)
			} else {
				log("resetting " + p.Name + " (removing named volumes)...")
				jerr = eng.Reset(context.Background(), p)
			}
			if jerr == nil && s.SyncRoutes != nil {
				s.SyncRoutes()
			}
			return jerr
		})
		writeJSON(w, http.StatusAccepted, JobRef{Job: job.Snapshot()})
		return
	}

	unlock := s.lockProject(p.Name)
	switch action {
	case "start":
		err = s.Engine.Up(r.Context(), p)
	case "stop":
		err = s.Engine.Down(r.Context(), p)
	case "restart":
		err = s.Engine.Restart(r.Context(), p)
	case "repair":
		err = s.Engine.Repair(r.Context(), p)
	default:
		unlock()
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown action %q", action))
		return
	}
	unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProjectVolumes lists a project's named volumes (Reset blast radius).
func (s *Server) handleProjectVolumes(w http.ResponseWriter, r *http.Request) {
	p, err := s.findProject(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	vols, err := s.Engine.Volumes(r.Context(), p)
	if err != nil {
		// Surface the error , a destructive Reset must not be confirmed
		// against a misleading empty "nothing to delete" list.
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if vols == nil {
		vols = []string{}
	}
	writeJSON(w, http.StatusOK, vols)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	job := s.Jobs.Start("create:"+req.Name, func(log func(string)) error {
		defer s.lockProject(req.Name)()
		jobEngine := s.NewJobEngine(log)
		// Jobs outlive the request that started them , background context.
		dir, err := jobEngine.NewProject(context.Background(), engine.NewOptions{
			Name:      req.Name,
			Template:  req.Template,
			DB:        req.DB,
			DBVersion: req.DBVersion,
			Redis:     req.Redis,
			PHP:       req.PHP,
			Version:   req.Version,
			Serve:     req.Serve,
			SkipStart: req.SkipStart,
		})
		if err != nil {
			return err
		}
		log("created " + dir)
		// Install the route + hosts entry now, so the site is reachable
		// immediately instead of 503-ing until the next lifecycle action.
		if !req.SkipStart && s.SyncRoutes != nil {
			s.SyncRoutes()
		}
		return nil
	})
	writeJSON(w, http.StatusAccepted, JobRef{Job: job.Snapshot()})
}

func (s *Server) handleAdoptCluster(w http.ResponseWriter, r *http.Request) {
	var req AdoptClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	m, err := s.Engine.AdoptCluster(engine.ClusterOptions{
		Dir:          req.Dir,
		Name:         req.Name,
		ComposeRoot:  req.ComposeRoot,
		ComposeFiles: req.ComposeFiles,
		Profiles:     req.Profiles,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": m.Name})
}

func (s *Server) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	var req CreateClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	specs := make([]engine.ContainerSpec, 0, len(req.Containers))
	for _, c := range req.Containers {
		svcs := make([]engine.ClusterServiceSpec, 0, len(c.Services))
		for _, sv := range c.Services {
			svcs = append(svcs, engine.ClusterServiceSpec{Engine: sv.Engine, Version: sv.Version})
		}
		specs = append(specs, engine.ContainerSpec{
			Name: c.Name, Template: c.Template, Image: c.Image, Version: c.Version, Port: c.Port, Serve: c.Serve, Services: svcs,
		})
	}
	job := s.Jobs.Start("create-cluster:"+req.Name, func(log func(string)) error {
		eng := s.NewJobEngine(log)
		dir, err := eng.NewCluster(context.Background(), engine.NewClusterOptions{
			Name: req.Name, Root: req.Root, ComposeRoot: req.ComposeRoot, Managed: req.Managed, Containers: specs, SkipStart: req.NoStart,
		})
		if err != nil {
			return err
		}
		log("created cluster at " + dir)
		if s.SyncRoutes != nil {
			s.SyncRoutes()
		}
		return nil
	})
	writeJSON(w, http.StatusAccepted, JobRef{Job: job.Snapshot()})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := state.Find(s.Config.Roots, req.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if p.Manifest != nil {
		writeError(w, http.StatusConflict, fmt.Errorf("%s is already managed by Hull", p.Name))
		return
	}
	job := s.Jobs.Start("import:"+p.Name, func(log func(string)) error {
		defer s.lockProject(p.Name)()
		jobEngine := s.NewJobEngine(log)
		if err := jobEngine.ImportExisting(context.Background(), p, log); err != nil {
			return err
		}
		if s.SyncRoutes != nil {
			s.SyncRoutes()
		}
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

// handleStopAll brings down everything Hull started , managed projects,
// adopted clusters (even out-of-root, via the started ledger), label-tagged
// orphans, and running shared services , so nothing keeps holding ports after
// the daemon stops. The logic lives in the engine (core-first); the handler
// just runs it and reconciles routes. Synchronous so the caller knows when
// the machine is clear.
func (s *Server) handleStopAll(w http.ResponseWriter, r *http.Request) {
	stopped, _ := s.Engine.StopAll(r.Context())
	if s.SyncRoutes != nil {
		s.SyncRoutes()
	}
	writeJSON(w, http.StatusOK, map[string]int{"stopped": stopped})
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
