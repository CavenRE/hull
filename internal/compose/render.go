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
	// permFixWP is where the wordpress entrypoint wrapper executes Hull's
	// permission script; permFixSSU is the serversideup entrypoint.d slot, which
	// that image sources before it starts the web server. "16-" runs just after
	// Hull's composer install (15-) so vendor exists first.
	permFixWP  = "/usr/local/bin/hull-fix-perms.sh"
	permFixSSU = "/etc/entrypoint.d/16-hull-writable.sh"
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
		if def, ok := templates.Site(m.Template); ok {
			for _, nv := range def.NamedVolumes {
				if f.Volumes == nil {
					f.Volumes = map[string]*Volume{}
				}
				f.Volumes[nv.Name] = nil
			}
		}
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
		if len(eng.HealthTest) > 0 {
			svc.HealthCheck = dbHealthCheck(eng.HealthTest)
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

	// A site's web container waits for its dedicated database to become healthy
	// before it starts, so the post_create migrate hook never races an unready
	// database and comes up with an empty schema. Relies on the db healthcheck
	// emitted above.
	if m.Type == manifest.TypeSite {
		if app := f.Services["app"]; app != nil {
			for _, key := range m.ServiceKeys() {
				s := m.Services[key]
				if s.Mode != manifest.ModeDedicated {
					continue
				}
				if eng, ok := templates.Engine(s.Engine); ok && len(eng.HealthTest) > 0 {
					if app.DependsOn == nil {
						app.DependsOn = map[string]DependsOn{}
					}
					app.DependsOn[key] = DependsOn{Condition: "service_healthy"}
				}
			}
		}
	}

	// Stamp Hull's ownership label on every generated service so the daemon
	// can reliably find and stop everything Hull started (engine.StopAll).
	for _, svc := range f.Services {
		svc.Labels = append([]string{ManagedLabel}, svc.Labels...)
	}

	return f, nil
}

// dbHealthCheck builds a database service healthcheck from the engine's probe
// command, with timing generous enough for a cold init over a slow bind mount
// but bounded so a wrong probe fails `up` in about a minute rather than hanging.
func dbHealthCheck(test []string) *HealthCheck {
	return &HealthCheck{
		Test:        test,
		Interval:    "5s",
		Timeout:     "5s",
		Retries:     12,
		StartPeriod: "10s",
	}
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
			"./:" + def.MountTarget(),
		},
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		Networks:   []string{"default"},
	}
	// OPcache is PHP-only; a non-PHP runtime (static, and later python/node/go)
	// gets no ini mount, no id-remap (skipped by ServersideUp below), and runs
	// the template's own image command.
	if def.IsPHP() {
		svc.Volumes = append(svc.Volumes, opcacheMount(ctx))
	}
	// A template whose vendor/ lives on a named volume needs that empty volume
	// filled before PHP-FPM serves; mount Hull's composer-install script into the
	// serversideup entrypoint.d, which runs it (once) at container init.
	if def.SeedsComposer() {
		svc.Volumes = append(svc.Volumes, composerInstallMount(ctx))
	}
	// Re-assert ownership of the paths the web user must write (WordPress
	// uploads, Laravel storage), on every boot, as root, before the server
	// serves. Mounted read-only; the path list travels in HULL_WRITABLE_PATHS.
	if def.NeedsPermFix() {
		svc.Volumes = append(svc.Volumes, permFixMount(ctx, def))
	}
	if def.Command != "" {
		svc.Command = def.Command
	}
	if def.Workdir != "" {
		svc.WorkingDir = def.Workdir
	}
	// Named volumes keep a heavy or platform-specific tree (e.g. a Python venv)
	// off the slow bind mount; Render registers them in the top-level volumes.
	for _, nv := range def.NamedVolumes {
		svc.Volumes = append(svc.Volumes, nv.Name+":"+nv.Path)
	}
	// Unserved sites (serve: false) still build and run; they just get no
	// loopback publish, no caddy route, and no caddy network membership.
	if m.Served() {
		svc.Ports = []string{loopbackPublish(def.UpstreamPort)}
		svc.Labels = caddyLabels(m.Domain+"."+ctx.TLD, def.UpstreamPort)
		svc.Networks = append(svc.Networks, caddyNetwork)
	}

	if m.Template == "wordpress" {
		// Order matters here. docker-ensure-installed.sh is the image's own
		// install-only entrypoint: it extracts WordPress core and returns. Running
		// it first means the permission pass sees a populated wp-content instead
		// of the empty directory that is all that exists on a first boot, which is
		// why the previous pre-entrypoint step could never have fixed uploads.
		// Then hand off to the image entrypoint to start Apache. This replaces a
		// recursive `chmod -R a+rX` of the whole webroot that cost ~7s per boot
		// over a 9p mount and only ever granted read, never the write bit that
		// WordPress media uploads need.
		svc.Entrypoint = []string{"bash", "-c", "docker-ensure-installed.sh; sh " + permFixWP + "; exec docker-entrypoint.sh \"$@\"", "--"}
		svc.Command = "apache2-foreground"
	}

	applyIDRemap(svc, ctx, def)
	// A Composer-seeding template must run its container init as root so the
	// entrypoint script can populate the fresh, root-owned vendor volume (a new
	// named volume mountpoint is owned by root, and serversideup runs rootless as
	// www-data by default, which cannot write it). serversideup drops php-fpm
	// workers back to www-data regardless. On native Linux, id-remap already runs
	// it as root; this covers Docker Desktop, where Hull otherwise leaves the
	// container as the image's www-data. Scoped to type:site, which mounts the
	// vendor volume (a type:app container does not, so it must not be forced root).
	if def.SeedsComposer() && svc.User == "" {
		svc.User = "0:0"
	}
	env := append([]string{}, def.ExtraEnv...)
	if def.NeedsPermFix() {
		env = append(env, "HULL_WRITABLE_PATHS="+strings.Join(def.WritablePaths, " "))
	}
	if m.Template == "wordpress" {
		if ctx.HostUID != "" {
			// Native Linux: run Apache, and therefore PHP, as the host user so the
			// files WordPress writes stay editable by the developer. This is the
			// wordpress twin of the serversideup id-remap, which cannot apply here
			// because the upstream image has no PUID helper. The image accepts the
			// "#1000" numeric form and Hull's permission script resolves the same
			// identity, so ownership agrees on every platform.
			env = append(env,
				"APACHE_RUN_USER=#"+ctx.HostUID,
				"APACHE_RUN_GROUP=#"+ctx.HostGID,
			)
		}
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
	// Non-PHP app runtimes (python, node, go) read their database connection from
	// DATABASE_URL; static has no runtime and PHP wires its own framework env.
	if !def.IsPHP() && def.Runtime != "static" {
		if dbKey, db, ok := m.DatabaseService(); ok {
			host := dbKey
			if db.Mode == manifest.ModeShared {
				host = templates.InstanceContainerName(db.Engine, db.Version)
			}
			if url := databaseURL(db.Engine, host, db.Database); url != "" {
				env = append(env, "DATABASE_URL="+url)
			}
		}
	}
	svc.Environment = mergeEnv(env, m.Env, nil)
	return svc, nil
}

// databaseURL builds a standard connection URL a non-PHP app (e.g. Python's
// dj-database-url / psycopg) reads from DATABASE_URL. Local dev uses the
// trust-auth superuser with no password.
func databaseURL(engine, host, dbname string) string {
	switch engine {
	case "postgres":
		return "postgres://postgres@" + host + ":5432/" + dbname
	case "mysql", "mariadb":
		return "mysql://root@" + host + ":3306/" + dbname
	}
	return ""
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

// composerInstallMount returns the read-only bind mount that drops Hull's
// composer-install script into the serversideup entrypoint.d, so a template
// whose vendor/ lives on a named volume (laravel) gets that empty volume filled
// with `composer install` before the web server starts. The "15-" prefix runs
// it after the image's own webserver-config init and before the Laravel
// automations. EnsureSystemFiles writes the host script before any container
// starts, and never overwrites a user-edited copy.
func composerInstallMount(ctx Context) string {
	host := ctx.HullHome + "/system/php/hull-composer-install.sh"
	return host + ":/etc/entrypoint.d/15-hull-composer-install.sh:ro"
}

// permFixMount returns the read-only bind mount that drops Hull's permission
// script into the container. serversideup sources every /etc/entrypoint.d/*.sh
// before it starts php-fpm, so it lands there; the wordpress image does not use
// entrypoint.d, so it lands on PATH and the entrypoint wrapper executes it.
// EnsureSystemFiles writes the host script before any container starts.
func permFixMount(ctx Context, def templates.SiteDef) string {
	target := permFixWP
	if def.ServersideUp() {
		target = permFixSSU
	}
	return ctx.HullHome + "/system/php/hull-fix-perms.sh:" + target + ":ro"
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
