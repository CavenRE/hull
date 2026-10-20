package api

import (
	"encoding/json"
	"net/http"

	"github.com/CavenRE/hull/internal/groups"
	"github.com/CavenRE/hull/internal/state"
)

// registerGroupRoutes adds the virtual-group endpoints. Groups are Hull-side
// organizational labels keyed by project directory (no project files change).
func (s *Server) registerGroupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/groups", s.handleGroupsGet)
	mux.HandleFunc("PUT /v1/groups", s.handleGroupsPut)
	mux.HandleFunc("POST /v1/projects/{name}/group", s.handleProjectGroup)
}

func (s *Server) handleGroupsGet(w http.ResponseWriter, r *http.Request) {
	store, err := groups.Load(s.cfg().HullHome)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, store)
}

func (s *Server) handleGroupsPut(w http.ResponseWriter, r *http.Request) {
	var store groups.Store
	if err := json.NewDecoder(r.Body).Decode(&store); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	saved, err := groups.Update(s.cfg().HullHome, func(st *groups.Store) error {
		*st = store
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// handleProjectGroup assigns one project to a group (empty = ungroup) without
// rewriting the whole document , the drag/drop + context-menu path.
func (s *Server) handleProjectGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := state.Find(s.cfg().Roots, r.PathValue("name"), s.cfg().Projects...)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if _, err := groups.Update(s.cfg().HullHome, func(st *groups.Store) error {
		st.SetMember(p.Dir, req.Group)
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
