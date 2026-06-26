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
	return os.WriteFile(s.path(), data, 0o644)
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

// runHooks runs an event's hooks in order. gated retries each hook briefly so
// it can wait for dependencies to come up (the general form of the old
// laravelMigrate poll). A failing hook aborts with an error unless it set
// ignore_failure; when:once / when:changed hooks consult the per-project state
// and may skip.
func (e *Engine) runHooks(ctx context.Context, p *state.Project, event string, gated bool) error {
	hooks := hooksFor(p, event)
	if len(hooks) == 0 {
		return nil
	}
	st := loadHookState(e.Config.HullHome, projectName(p))
	for i, h := range hooks {
		if h.Run == "" {
			continue
		}
		key := event + ":" + strconv.Itoa(i)
		hash := hookHash(h.Run)
		switch h.When {
		case "once":
			if _, ok := st.Done[key]; ok {
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
		if err := e.execHook(ctx, p, service, h.Run, gated); err != nil {
			if h.IgnoreFailure {
				continue
			}
			return fmt.Errorf("hook %s (%s): %w", event, h.Run, err)
		}
		st.Done[key] = hash
	}
	return st.save()
}

// execHook runs one hook command inside a service container via `compose exec
// -T sh -c`. When gated it retries , services may still be warming up after a
// start.
func (e *Engine) execHook(ctx context.Context, p *state.Project, service, run string, gated bool) error {
	attempts := 1
	if gated {
		attempts = 10
	}
	var err error
	for i := 0; i < attempts; i++ {
		if err = e.composeFor(p).ExecNoTTY(ctx, service, "sh", "-c", run); err == nil {
			return nil
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	return err
}
