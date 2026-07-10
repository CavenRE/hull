package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// adminerServer is one database entry shown in the Adminer picker at db.<tld>.
// Host is the container's name on the shared caddy network, where Adminer runs,
// so it can reach both shared instances and per-project DBs by name.
type adminerServer struct {
	Label  string `json:"label"`
	Engine string `json:"engine"`
	Driver string `json:"driver"` // Adminer login param: "server" (mysql/mariadb) or "pgsql"
	Host   string `json:"host"`
	Port   string `json:"port"`
	User   string `json:"user"`
	DB     string `json:"db,omitempty"`
}

// adminerDriver maps a Hull engine to the Adminer login URL parameter that
// selects its driver ("?pgsql=host" vs "?server=host").
func adminerDriver(engine string) string {
	if engine == "postgres" {
		return "pgsql"
	}
	return "server" // mysql & mariadb use Adminer's default MySQL driver
}

// dbUser is the trust-auth superuser Hull's local databases expose.
func dbUser(engine string) string {
	if engine == "postgres" {
		return "postgres"
	}
	return "root" // mysql, mariadb
}

// adminerServers enumerates every reachable database: each shared instance and
// each project's dedicated DB container. Shared-mode project DBs live inside a
// shared instance and are reached through that instance's entry, so they are
// not listed again.
func (e *Engine) adminerServers(ctx context.Context) []adminerServer {
	var out []adminerServer
	seen := map[string]bool{}
	add := func(s adminerServer) {
		key := s.Driver + "|" + s.Host
		if s.Host == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}

	// Shared DB instances (hull-postgres-16, hull-mysql-8.0, ...).
	m := services.NewManager(e.Config)
	m.Run = e.Run
	if insts, err := m.List(ctx); err == nil {
		for _, in := range insts {
			def, ok := templates.Engine(in.Engine)
			if !ok || !def.IsDatabase {
				continue
			}
			add(adminerServer{
				Label:  in.Name,
				Engine: in.Engine,
				Driver: adminerDriver(in.Engine),
				Host:   in.Container, // "hull-<name>"
				Port:   def.DefaultPort(),
				User:   dbUser(in.Engine),
			})
		}
	}

	// Per-project dedicated DB containers (<project>-<service>-1).
	if projects, err := state.Scan(e.Config.Roots); err == nil {
		for i := range projects {
			p := &projects[i]
			if p.Manifest == nil {
				continue
			}
			key, db, has := p.Manifest.DatabaseService()
			if !has || db.Mode == manifest.ModeShared {
				continue
			}
			def, ok := templates.Engine(db.Engine)
			if !ok || !def.IsDatabase {
				continue
			}
			add(adminerServer{
				Label:  p.Name,
				Engine: db.Engine,
				Driver: adminerDriver(db.Engine),
				Host:   projectName(p) + "-" + key + "-1",
				Port:   def.DefaultPort(),
				User:   dbUser(db.Engine),
				DB:     db.Database,
			})
		}
	}
	return out
}

// adminerSystemDir is where Hull's Adminer support files live (mounted into the
// adminer container).
func (e *Engine) adminerSystemDir() string {
	return filepath.Join(e.Config.HullHome, "system", "adminer")
}

// SyncAdminerServers regenerates the Adminer picker list (servers.json) from the
// current set of databases. Safe to call whenever databases change; the plugin
// reads it live, so no Adminer restart is needed.
func (e *Engine) SyncAdminerServers(ctx context.Context) error {
	dir := e.adminerSystemDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e.adminerServers(ctx), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "servers.json"), data, 0o644)
}

// EnsureAdminer provisions the Adminer console when auto-provisioning is enabled
// and at least one database exists, then refreshes the picker list. It is a
// no-op when auto-provisioning is off or there are no databases yet. Callers
// invoke it after a database is attached (services add, link, new --db).
func (e *Engine) EnsureAdminer(ctx context.Context) error {
	// Always refresh the list so a newly-attached DB shows up, even when
	// auto-provisioning is off (the user may have added Adminer manually).
	if err := e.SyncAdminerServers(ctx); err != nil {
		return err
	}
	if !e.Config.AutoAdminerEnabled() {
		return nil
	}
	if len(e.adminerServers(ctx)) == 0 {
		return nil
	}
	m := services.NewManager(e.Config)
	m.Run = e.Run
	_, err := m.EnsureUp(ctx, "adminer", "latest")
	return err
}
