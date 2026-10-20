package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/CavenRE/hull/internal/ledger"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/trust"
)

// UntrustedError is what Hull returns instead of running a project the user
// has not approved in its current form.
type UntrustedError struct {
	Project string
	Dir     string
	// Changed means the project was approved once, and its files changed since.
	Changed bool
}

func (e *UntrustedError) Error() string {
	why := "is not approved yet"
	if e.Changed {
		why = "changed since it was approved"
	}
	return fmt.Sprintf("%s (%s) %s; review what it runs with `hull allow %s`", e.Project, e.Dir, why, e.Project)
}

// maxTrustFile caps each file read for a fingerprint, and maxTrustRefs the
// files a compose stack can pull in through include and extends. A stack past
// either limit cannot be approved, so no part of it runs unchecked.
const (
	maxTrustFile = 4 << 20
	maxTrustRefs = 64
)

// trustFiles lists the files whose content decides what Hull runs for p:
// hull.yaml, and for a cluster or legacy project everything docker compose
// reads for it (stackInputs). A site's compose.yaml is not one: Hull
// re-renders it from hull.yaml before every start.
func (e *Engine) trustFiles(p *state.Project) ([]string, error) {
	var files []string
	if p.Manifest != nil {
		files = append(files, filepath.Join(p.Dir, manifest.Filename))
	}
	if p.Manifest == nil || isCluster(p) {
		compose, env, err := e.stackInputs(p)
		if err != nil {
			return nil, err
		}
		files = append(append(files, compose...), env...)
	}
	return files, nil
}

// stackInputs lists what docker compose reads for a cluster or legacy
// project: the pinned compose files and the ones they include or extend, and
// the env files that fill their ${VAR} references (--env-file, else the .env
// beside the first compose file, where compose looks for it).
func (e *Engine) stackInputs(p *state.Project) (compose, env []string, err error) {
	c := e.composeFor(p)
	for _, f := range c.Files {
		if !filepath.IsAbs(f) {
			f = filepath.Join(c.Dir, f)
		}
		compose = append(compose, f)
	}
	switch {
	case c.EnvFile != "":
		env = append(env, c.EnvFile)
	case len(compose) > 0:
		env = append(env, filepath.Join(filepath.Dir(compose[0]), ".env"))
	}
	seen := map[string]bool{}
	for _, f := range append(append([]string{}, compose...), env...) {
		seen[f] = true
	}
	var refs, refEnv []string
	for queue := append([]string{}, compose...); len(queue) > 0; queue = queue[1:] {
		files, envs := composeRefs(queue[0])
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				refs = append(refs, f)
				queue = append(queue, f)
			}
		}
		for _, f := range envs {
			if !seen[f] {
				seen[f] = true
				refEnv = append(refEnv, f)
			}
		}
		if len(refs)+len(refEnv) > maxTrustRefs {
			return nil, nil, fmt.Errorf("its compose files pull in more than %d other files", maxTrustRefs)
		}
	}
	sort.Strings(refs)
	sort.Strings(refEnv)
	return append(compose, refs...), append(env, refEnv...), nil
}

// composeRefs returns the local files a compose file pulls in: include paths
// and extends files, and the env file of each include. Paths are from the
// file's own folder. A remote include is skipped: its reference is text in
// the including file, which the fingerprint covers.
func composeRefs(file string) (files, envs []string) {
	data, err := readTrustFile(file)
	if err != nil {
		return nil, nil
	}
	var doc struct {
		Include  []yaml.Node `yaml:"include"`
		Services map[string]struct {
			Extends yaml.Node `yaml:"extends"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return nil, nil
	}
	dir := filepath.Dir(file)
	local := func(p string) (string, bool) {
		if p == "" || strings.Contains(p, "://") || strings.HasPrefix(p, "git@") {
			return "", false
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return filepath.Clean(p), true
	}
	for _, n := range doc.Include {
		var inc struct {
			Path       yamlList `yaml:"path"`
			EnvFile    yamlList `yaml:"env_file"`
			ProjectDir string   `yaml:"project_directory"`
		}
		if n.Kind == yaml.ScalarNode {
			inc.Path = yamlList{n.Value}
		} else if n.Decode(&inc) != nil {
			continue
		}
		for _, p := range inc.Path {
			if f, ok := local(p); ok {
				files = append(files, f)
			}
		}
		if len(inc.EnvFile) == 0 && len(inc.Path) > 0 {
			if first, ok := local(inc.Path[0]); ok {
				projectDir := filepath.Dir(first)
				if inc.ProjectDir != "" {
					projectDir, _ = local(inc.ProjectDir)
				}
				inc.EnvFile = yamlList{filepath.Join(projectDir, ".env")}
			}
		}
		for _, p := range inc.EnvFile {
			if f, ok := local(p); ok {
				envs = append(envs, f)
			}
		}
	}
	for _, svc := range doc.Services {
		var ext struct {
			File string `yaml:"file"`
		}
		if svc.Extends.Kind == yaml.MappingNode && svc.Extends.Decode(&ext) == nil {
			if f, ok := local(ext.File); ok {
				files = append(files, f)
			}
		}
	}
	return files, envs
}

// yamlList reads a YAML string or a list of strings.
type yamlList []string

func (l *yamlList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*l = yamlList{n.Value}
		return nil
	}
	var s []string
	err := n.Decode(&s)
	*l = s
	return err
}

// readTrustFile reads a file the way docker compose does, following a
// symbolic link, so its content is what the fingerprint covers. Anything but
// a regular file of at most maxTrustFile bytes is an error.
func readTrustFile(f string) ([]byte, error) {
	fi, err := os.Stat(f)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", f)
	}
	file, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxTrustFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTrustFile {
		return nil, fmt.Errorf("%s is larger than %d MiB", f, maxTrustFile>>20)
	}
	return data, nil
}

// Fingerprint hashes the files that decide what Hull runs for p. A file that
// is missing hashes as missing, so creating it later changes the result. It
// fails for a project Hull cannot cover in full (a file that is not regular
// or is over the cap, or too many referenced files), which keeps the project
// unapproved.
func (e *Engine) Fingerprint(p *state.Project) (string, error) {
	files, err := e.trustFiles(p)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range files {
		rel, err := filepath.Rel(p.Dir, f)
		if err != nil {
			rel = f
		}
		_, _ = fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		data, err := readTrustFile(f)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			_, _ = fmt.Fprintf(h, "missing\x00")
		case err != nil:
			return "", err
		default:
			_, _ = h.Write(data)
			_, _ = fmt.Fprintf(h, "\x00%d\x00", len(data))
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Trusted reports whether the user approved p in its current form.
func (e *Engine) Trusted(p *state.Project) bool {
	return e.requireTrust(p) == nil
}

// Trust records the user's approval of p in its current form.
func (e *Engine) Trust(p *state.Project) error {
	fp, err := e.Fingerprint(p)
	if err != nil {
		return fmt.Errorf("%s cannot be approved: %w", p.Name, err)
	}
	e.seedTrust()
	return trust.Set(e.cfg().HullHome, map[string]string{p.Dir: fp})
}

// requireTrust is the gate in front of everything that runs a project's
// containers or hooks.
func (e *Engine) requireTrust(p *state.Project) error {
	fp, err := e.Fingerprint(p)
	if err != nil {
		return fmt.Errorf("%s (%s) cannot be approved: %w", p.Name, p.Dir, err)
	}
	e.seedTrust()
	stored, known := trust.All(e.cfg().HullHome)[trust.Key(p.Dir)]
	if known && stored == fp {
		return nil
	}
	return &UntrustedError{Project: p.Name, Dir: p.Dir, Changed: known}
}

// Routable keeps the projects the user approved at some point. The router and
// hosts file never carry names for a folder that only appeared on disk; a
// later edit of an approved project keeps its routes until it is restarted.
func (e *Engine) Routable(projects []state.Project) []state.Project {
	e.seedTrust()
	known := trust.All(e.cfg().HullHome)
	var out []state.Project
	for _, p := range projects {
		if _, ok := known[trust.Key(p.Dir)]; ok {
			out = append(out, p)
		}
	}
	return out
}

// seedTrust creates the trust store on first use. It approves the projects
// the started ledger shows Hull already ran before approvals existed, so an
// upgrade does not stop or unroute them. A fresh install approves nothing.
func (e *Engine) seedTrust() {
	home := e.cfg().HullHome
	if trust.Exists(home) {
		return
	}
	entries := map[string]string{}
	for _, le := range ledger.List(home) {
		p := &state.Project{Name: le.Name, Dir: le.Dir}
		if m, err := manifest.Load(le.Dir); err == nil {
			p.Manifest = m
		}
		if fp, err := e.Fingerprint(p); err == nil {
			entries[le.Dir] = fp
		}
	}
	_ = trust.Seed(home, entries)
}

// TrustSummary describes, one line each, what running p will do, for the
// prompt that asks the user to approve it.
func (e *Engine) TrustSummary(p *state.Project) []string {
	var out []string
	m := p.Manifest
	if m == nil || isCluster(p) {
		compose, env, err := e.stackInputs(p)
		if err != nil {
			return append(out, "cannot be approved: "+err.Error())
		}
		out = append(out, "runs these compose files as written, with any image, host mount or privilege they ask for: "+strings.Join(relPaths(p.Dir, compose), ", "))
		var present []string
		for _, f := range env {
			if _, err := os.Stat(f); err == nil {
				present = append(present, f)
			}
		}
		if len(present) > 0 {
			out = append(out, "fills their ${VAR} values from: "+strings.Join(relPaths(p.Dir, present), ", "))
		}
	}
	if m == nil {
		return out
	}
	hooks := []struct {
		event string
		list  []manifest.Hook
	}{
		{"post_create", m.Hooks.PostCreate}, {"post_import", m.Hooks.PostImport},
		{"pre_up", m.Hooks.PreUp}, {"post_up", m.Hooks.PostUp}, {"pre_down", m.Hooks.PreDown},
		{"post_rebuild", m.Hooks.PostRebuild}, {"post_reset", m.Hooks.PostReset},
	}
	for _, h := range hooks {
		for _, hk := range h.list {
			out = append(out, "runs the "+h.event+" hook: "+hk.Run)
		}
	}
	for _, key := range m.ContainerKeys() {
		c := m.Containers[key]
		switch {
		case c.Build != "":
			out = append(out, fmt.Sprintf("builds container %s from %s", key, c.Build))
		case c.Image != "":
			out = append(out, fmt.Sprintf("runs container %s from image %s", key, c.Image))
		}
		if c.Template != "" && c.Path != "" && c.Path != "./" {
			out = append(out, fmt.Sprintf("mounts %s into container %s", c.Path, key))
		}
	}
	if m.Autostarts() {
		out = append(out, "starts by itself whenever the Hull daemon starts")
	}
	var hosts []string
	for _, ep := range Endpoints(m, e.cfg().TLD) {
		hosts = append(hosts, ep.Hosts...)
	}
	if len(hosts) > 0 {
		out = append(out, "serves "+strings.Join(hosts, ", "))
	}
	if len(out) == 0 {
		out = append(out, "runs the "+m.Template+" template with no hooks")
	}
	return out
}

// relPaths shows files relative to dir where they are inside it.
func relPaths(dir string, files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f
		if rel, err := filepath.Rel(dir, f); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			out[i] = filepath.ToSlash(rel)
		}
	}
	return out
}
