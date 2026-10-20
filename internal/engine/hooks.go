package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/safefile"
	"github.com/CavenRE/hull/internal/state"
)

// hookState persists when:once / when:changed bookkeeping per project so a
// release-style hook (migrations, key bootstrap) runs only when it should.
// Keyed by "<event>:<index>" -> command hash at last successful run.
type hookState struct {
	home    string
	project string
	Done    map[string]string
}

func loadHookState(home, project string) *hookState {
	s := &hookState{home: home, project: project, Done: map[string]string{}}
	if data, err := os.ReadFile(s.path()); err == nil {
		_ = json.Unmarshal(data, &s.Done)
	}
	return s
}

func (s *hookState) path() string {
	return filepath.Join(s.home, "hooks", s.project+".json")
}

func (s *hookState) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Done, "", "  ")
	if err != nil {
		return err
	}
	return safefile.Write(s.path(), data, 0o644)
}

func hookHash(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

// hooksFor returns the hook list for a lifecycle event from p's manifest.
func hooksFor(p *state.Project, event string) []manifest.Hook {
	if p.Manifest == nil {
		return nil
	}
	h := p.Manifest.Hooks
	switch event {
	case "post_create":
		return h.PostCreate
	case "post_import":
		return h.PostImport
	case "pre_up":
		return h.PreUp
	case "post_up":
		return h.PostUp
	case "pre_down":
		return h.PreDown
	case "post_rebuild":
		return h.PostRebuild
	case "post_reset":
		return h.PostReset
	}
	return nil
}

// defaultHookService is the container a hook runs in when it names none: the
// web container for a site, the first container for an app. Clusters wrap
// arbitrary compose, so a hook there must name its service explicitly.
func defaultHookService(p *state.Project) string {
	if p.Manifest == nil {
		return "app"
	}
	switch p.Manifest.Type {
	case manifest.TypeApp:
		if keys := p.Manifest.ContainerKeys(); len(keys) > 0 {
			return keys[0]
		}
		return ""
	case manifest.TypeCluster:
		return ""
	default:
		return "app"
	}
}

// hookServices returns the services a hook can target: the ones Hull renders
// for a site or app, or the wrapped compose's for a cluster (nil when that
// compose cannot be read).
func hookServices(p *state.Project) map[string]bool {
	m := p.Manifest
	if m.Type == manifest.TypeCluster {
		return composeServiceNames(filepath.Join(p.Dir, m.ComposeRoot), m.ComposeFiles)
	}
	known := map[string]bool{}
	if m.Type == manifest.TypeSite {
		known["app"] = true
	}
	for key := range m.Containers {
		known[key] = true
	}
	for key, s := range m.Services {
		if s != nil && s.Mode == manifest.ModeDedicated {
			known[key] = true
		}
	}
	return known
}

// runHooks runs an event's hooks in order. gated first waits for the hook's
// service to accept commands (the general form of the old laravelMigrate
// poll). A failing hook aborts with an error unless it set ignore_failure;
// when:once / when:changed hooks consult the per-project state and may skip.
// pre_up hooks run before the containers do, so each gets a one-off
// container of its service.
func (e *Engine) runHooks(ctx context.Context, p *state.Project, event string, gated bool) error {
	if err := e.requireTrust(p); err != nil {
		return err
	}
	hooks := hooksFor(p, event)
	if len(hooks) == 0 {
		return nil
	}
	st := loadHookState(e.cfg().HullHome, projectName(p))
	for i, h := range hooks {
		if h.Run == "" {
			continue
		}
		hash := hookHash(h.Run)
		slot := event + ":" + strconv.Itoa(i)
		// once follows the command, so reordering the list neither re-runs nor
		// skips it; the slot key is what earlier versions stored.
		key := slot
		if h.When == "once" {
			key = event + ":" + hash
		}
		switch h.When {
		case "once":
			if _, ok := st.Done[key]; ok || st.Done[slot] == hash {
				continue
			}
		case "changed":
			if st.Done[key] == hash {
				continue
			}
		}
		service := h.Service
		if service == "" {
			service = defaultHookService(p)
		}
		if service == "" {
			return fmt.Errorf("hook %s[%d]: no target service , set `service:` (required for apps/clusters)", event, i)
		}
		if known := hookServices(p); known != nil && !known[service] {
			if h.IgnoreFailure {
				continue
			}
			return fmt.Errorf("hook %s[%d]: service %q is not in %s (services: %s)", event, i, service, projectName(p), sortedKeys(known))
		}
		if err := e.execHook(ctx, p, service, h.Run, gated, event == "pre_up"); err != nil {
			if h.IgnoreFailure {
				continue
			}
			return fmt.Errorf("hook %s (%s): %w", event, h.Run, err)
		}
		if h.When != "" {
			// Saved per hook, so a later hook's failure cannot make this one
			// run again.
			st.Done[key] = hash
			if err := st.save(); err != nil {
				return err
			}
		}
	}
	return nil
}

// execHook runs one hook command (`sh -c run`) in a service container: a
// one-off container, or exec in the running one. When gated it first waits
// for the service to accept commands; only that wait is retried, so a hook
// that runs and fails (a half-applied migration) is never run again.
func (e *Engine) execHook(ctx context.Context, p *state.Project, service, run string, gated, oneOff bool) error {
	c := e.composeFor(p)
	if oneOff {
		return c.RunOneOff(ctx, service, "sh", "-c", run)
	}
	if gated {
		var err error
		for i := 0; i < 10; i++ {
			if err = c.ExecNoTTY(ctx, service, "sh", "-c", "true"); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		if err != nil {
			return fmt.Errorf("service %s is not accepting commands: %w", service, err)
		}
	}
	return c.ExecNoTTY(ctx, service, "sh", "-c", run)
}
