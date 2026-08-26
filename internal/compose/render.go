package compose

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/templates"
)

// Context carries the machine/global settings a render needs. Generated
// compose files embed concrete values (paths, TLD) , they are disposable
// artifacts, regenerated whenever the manifest or settings change.
type Context struct {
	// TLD is the local top-level domain, e.g. "test".
	TLD string
	// HullHome is the absolute path to the Hull home directory (for the
	// shared xdebug.ini mount). Forward slashes work on all engines.
	HullHome string
	// HostUID/HostGID, when non-empty, are emitted as PUID/PGID on
	// serversideup/php containers so the in-container user matches the host
	// and can write bind-mounted files. Set only on native Linux Docker;
	// empty on macOS/Windows, where Docker Desktop remaps ownership.
	HostUID string
	HostGID string
}

// applyIDRemap makes a serversideup/php container write host-owned bind mounts
// on native Linux. The image bakes www-data as uid 33, which can't write files
// owned by the host user; its docker-php-serversideup-set-id helper remaps
// www-data to the host uid/gid but must run as root. So we start the container
// as root and wrap the normal entrypoint to remap first, then hand off to the
// image's own entrypoint (which runs the entrypoint.d scripts and drops fpm
// workers back to the now-correct www-data). No-op on non-Linux (Docker
// Desktop remaps in its VM) or for non-serversideup images.
func applyIDRemap(svc *ServiceDef, ctx Context, def templates.SiteDef) {
	if ctx.HostUID == "" || !def.ServersideUp() {
		return
	}
	setID := fmt.Sprintf("docker-php-serversideup-set-id www-data %s:%s 2>/dev/null || true", ctx.HostUID, ctx.HostGID)
	svc.User = "0:0"
	svc.Entrypoint = []string{"sh", "-c", setID + `; exec docker-php-serversideup-entrypoint "$@"`, "--"}
	svc.Command = "/init"
}

const (
	caddyNetwork = "caddy"
	webrootMount = "/var/www/html"
)

// ManagedLabel marks every container Hull generates as Hull-owned. The daemon
// uses it to find and stop everything Hull started , including rendered
// projects whose directory has moved or fallen outside the configured roots
// (see engine.StopAll). Adopted clusters are NOT rendered by Hull and so do
// not carry this label; they are tracked in the started ledger instead.
const ManagedLabel = "com.hull.managed=true"

// Render generates the compose file for a validated manifest.
func Render(m *manifest.Manifest, ctx Context) (*File, error) {
	if ctx.TLD == "" {
		ctx.TLD = "test"
	}
	f := &File{
		Name:     m.Name,
		Services: map[string]*ServiceDef{},
		Networks: map[string]*Network{caddyNetwork: {External: true}},
	}

	switch m.Type {
	case manifest.TypeSite:
		svc, err := siteService(m, ctx)
		if err != nil {
			return nil, err
		}
		f.Services["app"] = svc
	case manifest.TypeApp:
		for _, key := range m.ContainerKeys() {
			svc, err := containerService(m, key, m.Containers[key], ctx)
			if err != nil {
				return nil, err
			}
			f.Services[key] = svc
			// Define each private network a container joins, as an internal
			// bridge (no external:). Membership is what isolates a sensitive
			// backend to only the services that share its network.
			for _, n := range m.Containers[key].Networks {
				if _, ok := f.Networks[n]; !ok {
					f.Networks[n] = &Network{}
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported project type %q", m.Type)
	}

	for _, key := range m.ServiceKeys() {
		s := m.Services[key]
		if s.Mode != manifest.ModeDedicated {
			continue // shared instances live outside the project (Phase 5)
		}
		eng, ok := templates.Engine(s.Engine)
		if !ok {
			return nil, fmt.Errorf("service %q: unknown engine %q", key, s.Engine)
		}
		networks := []string{"default"}
		if eng.JoinsCaddy {
			networks = append(networks, caddyNetwork)
		}
		svc := &ServiceDef{
			Image:       eng.Image(s.Version),
			Command:     eng.Command,
			Environment: eng.Env(s.Database),
			Networks:    networks,
		}
		if eng.DataPath != "" {
			volume := key + "_data"
			svc.Volumes = []string{volume + ":" + eng.DataPath}
			if f.Volumes == nil {
				f.Volumes = map[string]*Volume{}
			}
			f.Volumes[volume] = nil
		}
		f.Services[key] = svc
	}

	// Stamp Hull's ownership label on every generated service so the daemon
	// can reliably find and stop everything Hull started (engine.StopAll).
	for _, svc := range f.Services {
		svc.Labels = append([]string{ManagedLabel}, svc.Labels...)
	}

	return f, nil
}

// siteService builds the single web container of a type:site project.
func siteService(m *manifest.Manifest, ctx Context) (*ServiceDef, error) {
	def, ok := templates.Site(m.Template)
	if !ok {
		return nil, fmt.Errorf("unknown template %q", m.Template)
	}
	svc := &ServiceDef{
		Image: def.Image(m.PHP, m.Version),
		Volumes: []string{
			"./:" + webrootMount,
			opcacheMount(ctx),
		},
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		Networks:   []string{"default"},
	}
	// Unserved sites (serve: false) still build and run; they just get no
	// loopback publish, no caddy route, and no caddy network membership.
	if m.Served() {
		svc.Ports = []string{loopbackPublish(def.UpstreamPort)}
		svc.Labels = caddyLabels(m.Domain+"."+ctx.TLD, def.UpstreamPort)
		svc.Networks = append(svc.Networks, caddyNetwork)
	}

	if m.Template == "wordpress" {
		// Best-effort fix for the Windows bind-mount Apache error
		// "unable to read htaccess file, denying access to be safe": make the
		// webroot world-readable on boot, then run the image's normal
		// entrypoint. (Wrapper pattern; chmod is a no-op on mounts that ignore
		// it, but resolves the denial where it applies.)
		svc.Entrypoint = []string{"bash", "-c", "chmod -R a+rX /var/www/html 2>/dev/null || true; exec docker-entrypoint.sh \"$@\"", "--"}
		svc.Command = "apache2-foreground"
	}

	applyIDRemap(svc, ctx, def)
	env := append([]string{}, def.ExtraEnv...)
	if m.Template == "wordpress" {
		dbKey, db, ok := m.DatabaseService(def.RequiredDB...)
		if !ok {
			return nil, fmt.Errorf("template wordpress requires a %s service", strings.Join(def.RequiredDB, " or "))
		}
		host := dbKey
		if db.Mode == manifest.ModeShared {
			// Shared instances are addressed by container name on the
			// shared (caddy) network.
			host = templates.InstanceContainerName(db.Engine, db.Version)
		}
		env = append(env,
			"WORDPRESS_DB_HOST="+host,
			"WORDPRESS_DB_NAME="+db.Database,
			"WORDPRESS_DB_PASSWORD=",
			"WORDPRESS_DB_USER=root",
			// Local-dev defaults: mark the environment local, and disable
			// page-load wp-cron so the dashboard stops firing blocking update
			// checks on every request (the usual cause of a slow first load).
			// A project can override either via its own hull.yaml env.
			"WP_ENVIRONMENT_TYPE=local",
			"WORDPRESS_CONFIG_EXTRA=define('DISABLE_WP_CRON', true);",
		)
	}
	svc.Environment = mergeEnv(env, m.Env, nil)
	return svc, nil
}

// containerService builds one container of a type:app project.
func containerService(m *manifest.Manifest, key string, c *manifest.Container, ctx Context) (*ServiceDef, error) {
	if c.Template != "" {
		def, ok := templates.Site(c.Template)
		if !ok {
			return nil, fmt.Errorf("container %q: unknown template %q", key, c.Template)
		}
		svc := &ServiceDef{
			Image:   def.Image(c.PHP, c.Version),
			Command: c.Command,
			Volumes: []string{
				mountSource(c.Path) + ":" + webrootMount,
				opcacheMount(ctx),
			},
			ExtraHosts:  []string{"host.docker.internal:host-gateway"},
			Environment: mergeEnv(def.ExtraEnv, m.Env, c.Env),
			Networks:    append([]string{"default"}, c.Networks...),
		}
		applyIDRemap(svc, ctx, def)
		if c.Domain != "" && c.Served() {
			port := def.UpstreamPort
			if c.Port != 0 {
				port = c.Port
			}
			svc.Ports = []string{loopbackPublish(port)}
			svc.Labels = caddyLabels(c.Domain+"."+ctx.TLD, port)
			svc.Networks = append(svc.Networks, caddyNetwork)
		}
		return svc, nil
	}

	svc := &ServiceDef{
		Image:       c.Image,
		Build:       c.Build,
		Command:     c.Command,
		Environment: mergeEnv(nil, m.Env, c.Env),
		Networks:    append([]string{"default"}, c.Networks...),
	}
	// A raw image opts into Hull's OPcache tuning with `php_tune: true` (Hull
	// cannot know a custom image is PHP, or where its conf.d lives, otherwise).
	if c.PHPTune {
		svc.Volumes = append(svc.Volumes, opcacheMount(ctx))
	}
	if c.Domain != "" && c.Served() {
		svc.Ports = []string{loopbackPublish(c.Port)}
		svc.Labels = caddyLabels(c.Domain+"."+ctx.TLD, c.Port)
		svc.Networks = append(svc.Networks, caddyNetwork)
	}
	return svc, nil
}

// loopbackPublish exposes a container port on an ephemeral 127.0.0.1 host
// port for the host-process router (ADR 0007). Loopback-only keeps dev
// sites off the LAN; the daemon discovers the assigned port after start.
func loopbackPublish(containerPort int) string {
	return fmt.Sprintf("127.0.0.1::%d", containerPort)
}

// caddyLabels routes a domain to the container via the Caddy ingress
// (caddy-docker-proxy label syntax, kept through Phase 3; the embedded
// router in Phase 4 consumes the same information from the manifest).
func caddyLabels(fqdn string, upstreamPort int) []string {
	return []string{
		"caddy=" + fqdn,
		fmt.Sprintf("caddy.reverse_proxy={{upstreams %d}}", upstreamPort),
		"caddy.tls=internal",
	}
}

// opcacheMount returns the read-only bind mount that drops Hull's shared
// opcache.ini into a PHP container's conf.d, so every PHP image (serversideup,
// the upstream wordpress image, and opted-in custom images) gets one uniform
// OPcache tuning instead of relying on each image's own defaults. Images ship
// OPcache off or under-sized, so every request otherwise recompiles and
// re-stats over the bind mount; the shared ini enables it, holds a large file
// set, and revalidates at most every 2s. The "zz-" prefix loads it after an
// image's own opcache ini (e.g. wordpress's opcache-recommended.ini), so Hull's
// settings win. EnsureSystemFiles writes the host file before any container
// starts, and never overwrites a copy the user has edited.
//
// Xdebug is deliberately not forced on: serversideup v4 images ship no xdebug
// extension, so Hull no longer mounts a `zend_extension=xdebug` ini, which only
// produced a "cannot load xdebug" warning on every PHP invocation.
func opcacheMount(ctx Context) string {
	host := ctx.HullHome + "/system/php/opcache.ini"
	return host + ":" + templates.PHPConfDir + "/zz-hull-opcache.ini:ro"
}

// mountSource normalizes a manifest path field to a compose bind source.
func mountSource(p string) string {
	cleaned := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if cleaned == "." || cleaned == "./" {
		return "./"
	}
	if !strings.HasPrefix(cleaned, "./") {
		cleaned = "./" + cleaned
	}
	return cleaned
}

// mergeEnv layers template env, project env, and container env (later wins
// per key) and returns sorted KEY=value pairs.
func mergeEnv(base []string, project, container map[string]string) []string {
	merged := map[string]string{}
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		merged[k] = v
	}
	for k, v := range project {
		merged[k] = v
	}
	for k, v := range container {
		merged[k] = v
	}
	if len(merged) == 0 {
		return nil
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
