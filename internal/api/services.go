package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// registerServiceRoutes adds the shared-services endpoints to the mux.
func (s *Server) registerServiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/services", s.handleServicesList)
	mux.HandleFunc("POST /v1/services", s.handleServiceAdd)
	mux.HandleFunc("POST /v1/services/{name}/{action}", s.handleServiceAction)
	mux.HandleFunc("DELETE /v1/services/{name}", s.handleServiceRemove)
}

func (s *Server) handleServicesList(w http.ResponseWriter, r *http.Request) {
	instances, err := s.Services().List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// Which projects consume which instance (manifest scan).
	linked := map[string][]string{}
	if projects, err := state.Scan(s.Config.Roots); err == nil {
		for i := range projects {
			m := projects[i].Manifest
			if m == nil {
				continue
			}
			for _, key := range m.ServiceKeys() {
				svc := m.Services[key]
				if svc.Mode != "shared" {
					continue
				}
				name := templates.InstanceName(svc.Engine, svc.Version)
				linked[name] = append(linked[name], projects[i].Name)
			}
		}
	}

	infos := make([]ServiceInfo, 0, len(instances))
	for _, in := range instances {
		info := ServiceInfo{
			Name:           in.Name,
			Engine:         in.Engine,
			Version:        in.Version,
			Container:      in.Container,
			Running:        in.Running,
			LinkedProjects: linked[in.Name],
		}
		if def, ok := templates.Engine(in.Engine); ok {
			if def.UISubdomain != "" {
				info.URL = "https://" + def.UISubdomain + "." + s.Config.TLD
			}
			if in.HostPort > 0 {
				info.Host = "127.0.0.1"
				info.HostPort = in.HostPort
				switch in.Engine {
				case "postgres":
					info.Username = "postgres"
				case "mysql", "mariadb":
					info.Username = "root"
				}
			}
		}
		infos = append(infos, info)
	}
	writeJSON(w, http.StatusOK, infos)
}

func (s *Server) handleServiceAdd(w http.ResponseWriter, r *http.Request) {
	var req AddServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, _, err := services.Resolve(req.Engine + "@" + req.Version); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	job := s.Jobs.Start("service:"+req.Engine, func(log func(string)) error {
		log(fmt.Sprintf("provisioning shared %s...", req.Engine))
		name, err := s.JobServices(log).Add(context.Background(), req.Engine, req.Version)
		if err != nil {
			return err
		}
		log("instance " + name + " is up")
		if s.SyncRoutes != nil {
			s.SyncRoutes()
		}
		return nil
	})
	writeJSON(w, http.StatusAccepted, JobRef{Job: job.Snapshot()})
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	switch action {
	case "start":
		if err := s.Services().Start(r.Context(), name); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	case "stop":
		if err := s.Services().Stop(r.Context(), name); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	case "link":
		s.handleServiceLink(w, r, name)
		return
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown action %q", action))
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleServiceLink(w http.ResponseWriter, r *http.Request, instance string) {
	var req LinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := state.Find(s.Config.Roots, req.Project)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	instances, err := s.Services().List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var spec string
	for _, in := range instances {
		if in.Name == instance {
			spec = in.Engine
			if in.Version != "" {
				spec += "@" + in.Version
			}
			break
		}
	}
	if spec == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("no shared instance %q", instance))
		return
	}

	job := s.Jobs.Start("link:"+req.Project, func(log func(string)) error {
		log(fmt.Sprintf("linking %s to %s...", req.Project, instance))
		eng := s.NewJobEngine(log)
		if _, err := eng.Link(context.Background(), p, spec, s.JobServices(log)); err != nil {
			return err
		}
		log("restarting " + req.Project + " to apply...")
		fresh, err := state.Find(s.Config.Roots, req.Project)
		if err != nil {
			return err
		}
		if err := eng.Up(context.Background(), fresh); err != nil {
			return err
		}
		if s.SyncRoutes != nil {
			s.SyncRoutes()
		}
		return nil
	})
	writeJSON(w, http.StatusAccepted, JobRef{Job: job.Snapshot()})
}

func (s *Server) handleServiceRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.Services().Remove(r.Context(), r.PathValue("name")); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}
