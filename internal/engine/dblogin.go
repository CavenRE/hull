package engine

import (
	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/dbauth"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/templates"
)

// sharedScope and projectScope name where a database login lives (dbauth).
func sharedScope(s *manifest.Service) string {
	return "instance/" + templates.InstanceName(s.Engine, s.Version)
}

func projectScope(m *manifest.Manifest) string { return "project/" + m.Name }

// dbLogin is the user and secret a project's code signs in with to one of its
// services: for a database its own login in a shared instance or the root
// password of its dedicated one, for a search or storage engine the
// instance's or its own API key. Nothing stored means a service made before
// Hull generated secrets, which keeps its passwordless root or fixed key.
func (e *Engine) dbLogin(m *manifest.Manifest, key string, s *manifest.Service) (string, string) {
	home := e.cfg().HullHome
	def, _ := templates.Engine(s.Engine)
	if s.Mode == manifest.ModeShared {
		name := "_root"
		if def.IsDatabase {
			name = s.Database
		}
		if l, ok := dbauth.Get(home, sharedScope(s), name); ok {
			return l.User, l.Password
		}
	} else if l, ok := dbauth.Get(home, projectScope(m), key); ok {
		return l.User, l.Password
	}
	return def.AdminUser(), def.LegacySecret()
}

// DBLogins returns, for a project's database service, the login a restore
// runs as (the project's own, so a dump cannot reach other projects' data)
// and the one Hull administers the database with (dumps, readiness).
func (e *Engine) DBLogins(m *manifest.Manifest, key string) (app, admin bundle.Auth) {
	s := m.Services[key]
	if s == nil {
		return bundle.Auth{}, bundle.Auth{}
	}
	user, password := e.dbLogin(m, key, s)
	app = bundle.Auth{User: user, Password: password}
	if s.Mode == manifest.ModeShared {
		// Inside a shared instance's container, root signs in through the
		// socket without a password.
		return app, bundle.Auth{User: templates.RootUser(s.Engine)}
	}
	return app, app
}

// ensureLogins gives each dedicated service of a new project that has an
// admin secret (a database root password, an API key) a generated one before
// its first render. Only for a new project: an initialized volume keeps the
// root it was created with.
func (e *Engine) ensureLogins(m *manifest.Manifest) error {
	for _, key := range m.ServiceKeys() {
		s := m.Services[key]
		if s.Mode == manifest.ModeShared {
			continue
		}
		def, ok := templates.Engine(s.Engine)
		if !ok || !def.HasSecret() {
			continue
		}
		if _, err := dbauth.Ensure(e.cfg().HullHome, projectScope(m), key, def.AdminUser()); err != nil {
			return err
		}
	}
	return nil
}
