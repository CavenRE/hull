package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/CavenRE/hull/internal/certs"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/router"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// registerSetupRoutes adds the GUI onboarding endpoints (Wave F): the
// wizard drives the same machinery as `hull setup`/`hull trust`.
func (s *Server) registerSetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/setup/trust", s.handleSetupTrust)
	mux.HandleFunc("POST /v1/setup/dns", s.handleSetupDNS)
	mux.HandleFunc("POST /v1/setup/reapply", s.handleSetupReapply)
	mux.HandleFunc("POST /v1/projects/{name}/unlink", s.handleProjectUnlink)
}

// handleSetupReapply re-applies the setup: system files, the local CA, trust
// and DNS registration (each of which can show an elevation prompt), and a
// route and hosts sync. A step that cannot run here is reported with the
// command to run in a terminal.
func (s *Server) handleSetupReapply(w http.ResponseWriter, r *http.Request) {
	var steps []ReapplyStep
	add := func(name, status, detail, manual string) {
		steps = append(steps, ReapplyStep{Name: name, Status: status, Detail: detail, Manual: manual})
	}
	dataDir := s.cfg().RouterDataDir()

	if err := templates.EnsureSystemFiles(s.cfg().HullHome); err != nil {
		add("System files", "warn", err.Error(), "")
	} else {
		add("System files", "ok", "provisioned", "")
	}

	if !certs.Trusted(dataDir) {
		if err := router.EnsureCA(dataDir); err != nil {
			add("Local CA", "warn", err.Error(), "")
		} else {
			add("Local CA", "ok", "provisioned", "")
		}
	} else {
		add("Local CA", "ok", "present", "")
	}

	// Trust install needs root; surface the command rather than failing.
	if err := certs.InstallTrust(dataDir); err != nil {
		add("Certificate trust", "manual", "needs elevation", "hull trust")
	} else {
		add("Certificate trust", "ok", "installed", "")
	}

	// DNS registration also needs elevation on most platforms.
	if s.cfg().DNS.Enabled {
		if err := platform.RegisterDNS(s.cfg().TLD, s.cfg().Router.Loopback, s.cfg().DNS.Port); err != nil {
			var manual *platform.ManualStepsError
			if errors.As(err, &manual) {
				add("DNS registration", "manual", "needs elevation", manual.Instructions)
			} else {
				add("DNS registration", "warn", err.Error(), "")
			}
		} else {
			add("DNS registration", "ok", "registered", "")
		}
	} else {
		add("DNS", "ok", "using an external resolver (Hull DNS disabled)", "")
	}

	if s.SyncRoutes != nil {
		s.SyncRoutes()
		add("Routes & hosts", "ok", "re-synced", "")
	}

	writeJSON(w, http.StatusOK, ReapplyResult{Steps: steps})
}

func (s *Server) handleSetupTrust(w http.ResponseWriter, r *http.Request) {
	dataDir := s.cfg().RouterDataDir()
	if !certs.Trusted(dataDir) {
		if err := router.EnsureCA(dataDir); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := certs.InstallTrust(dataDir); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, SetupStepResult{Done: true})
}

func (s *Server) handleSetupDNS(w http.ResponseWriter, r *http.Request) {
	if err := platform.RegisterDNS(s.cfg().TLD, s.cfg().Router.Loopback, s.cfg().DNS.Port); err != nil {
		var manual *platform.ManualStepsError
		if errors.As(err, &manual) {
			writeJSON(w, http.StatusOK, SetupStepResult{Done: false, Manual: manual.Instructions})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes() // hosts block follows immediately
	}
	writeJSON(w, http.StatusOK, SetupStepResult{Done: true})
}

func (s *Server) handleProjectUnlink(w http.ResponseWriter, r *http.Request) {
	var req UnlinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := state.Find(s.cfg().Roots, r.PathValue("name"), s.cfg().Projects...)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer s.lockProject(p.Name)()
	eng := engine.New(s.cfg())
	eng.Run = s.Engine.Run
	eng.EnsureNet = s.Engine.EnsureNet
	if err := eng.Unlink(r.Context(), p, req.Key); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.SyncRoutes != nil {
		go s.SyncRoutes()
	}
	w.WriteHeader(http.StatusNoContent)
}
