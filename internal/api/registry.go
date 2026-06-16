package api

import (
	"net/http"

	"github.com/CavenRE/hull/internal/registry"
	"github.com/CavenRE/hull/internal/templates"
)

// registerRegistryRoutes adds live Docker Hub lookups (version pickers and
// the App container search). All degrade to an empty list on error so the
// GUI falls back to its static catalogs.
func (s *Server) registerRegistryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/registry/versions", s.handleVersions)
	mux.HandleFunc("GET /v1/registry/php", s.handlePHP)
	mux.HandleFunc("GET /v1/registry/search", s.handleSearch)
	mux.HandleFunc("GET /v1/registry/tags", s.handleTags)
}

// handleTags lists selectable version tags for an arbitrary Docker Hub repo
// (the App-container flow). Always offers "latest" first, then the cleaned
// major versions Hub publishes.
func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeJSON(w, http.StatusOK, []string{"latest"})
		return
	}
	tags, err := s.Registry.Tags(r.Context(), repo)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{"latest"})
		return
	}
	if q := r.URL.Query().Get("q"); q != "" {
		writeJSON(w, http.StatusOK, registry.FilterTags(tags, q, 30))
		return
	}
	out := append([]string{"latest"}, registry.CleanVersions(tags, 8)...)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	engine := r.URL.Query().Get("engine")
	q := r.URL.Query().Get("q")
	def, ok := templates.Engine(engine)
	if !ok || def.Repo() == "" {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	tags, err := s.Registry.Tags(r.Context(), def.Repo())
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	if q != "" {
		writeJSON(w, http.StatusOK, registry.FilterTags(tags, q, 30))
		return
	}
	writeJSON(w, http.StatusOK, registry.CleanVersions(tags, 6))
}

func (s *Server) handlePHP(w http.ResponseWriter, r *http.Request) {
	// Pull X.Y from the official php image (clean, predictable tags) rather
	// than serversideup/php, whose recent tags are dominated by beta builds.
	// serversideup ships an -fpm-nginx variant for the same minors.
	tags, err := s.Registry.Tags(r.Context(), templates.PHPVersionRepo)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	if q := r.URL.Query().Get("q"); q != "" {
		writeJSON(w, http.StatusOK, registry.FilterTags(tags, q, 30))
		return
	}
	writeJSON(w, http.StatusOK, registry.MinorVersions(tags, 5))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, []registry.Repo{})
		return
	}
	repos, err := s.Registry.Search(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusOK, []registry.Repo{})
		return
	}
	writeJSON(w, http.StatusOK, repos)
}
