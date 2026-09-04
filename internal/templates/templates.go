package templates

import (
	"fmt"
	"sort"
)

// DefaultPHP is the PHP version used when a manifest does not pin one.
const DefaultPHP = "8.4"

// DefaultPython is the Python version used when a python project pins none.
const DefaultPython = "3.13"

// DefaultNode is the Node version used when a node project pins none.
const DefaultNode = "22"

// DefaultGo is the Go toolchain version used when a go project pins none.
const DefaultGo = "1.24"

// SiteDef describes a built-in site template: the web container Hull
// generates for it and how requests reach it. Ported from v1's
// templates/base/*.yaml (fixing plain's missing default network).
type SiteDef struct {
	Key          string
	UpstreamPort int
	// XdebugTarget is where a shared xdebug.ini would mount inside the
	// container. Reserved: Hull no longer mounts it, because serversideup v4
	// images ship no xdebug extension, so forcing zend_extension=xdebug only
	// printed a "cannot load xdebug" warning on every PHP call. See opcacheMount
	// in internal/compose/render.go for the ini Hull does mount.
	XdebugTarget string
	// ExtraEnv is template-fixed environment (KEY=value pairs).
	ExtraEnv []string
	// RequiredDB lists acceptable database engines when the template cannot
	// run without one (e.g. wordpress).
	RequiredDB []string
	// Runtime is the language family: "php" (laravel/plain/wordpress), or a
	// non-PHP runtime like "static", "python", "node", "go". It gates the
	// PHP-only render behavior (the OPcache mount, the serversideup id-remap,
	// the PHP image selection). Empty is treated as "php" for back-compat.
	Runtime string
	// BaseImage is the container image for a non-PHP template (PHP templates
	// compute their own image from the PHP version). May include a tag.
	BaseImage string
	// Mount is where the project directory is bind-mounted inside the container.
	// Empty defaults to the PHP webroot (/var/www/html).
	Mount string
	// Workdir is the container working directory for run commands (non-PHP
	// templates that execute a dev server); empty means the image default.
	Workdir string
	// Command overrides the container command (e.g. a dev server for a non-PHP
	// runtime); empty means the image default.
	Command string
	// NamedVolumes are docker named volumes the app service mounts, to keep a
	// heavy or platform-specific tree (a Python venv, a build cache) off the slow
	// bind mount. Compose scopes the volume name to the project.
	NamedVolumes []NamedVolume
}

// NamedVolume is a docker named volume a template's app service mounts.
type NamedVolume struct {
	Name string
	Path string
}

var sites = map[string]SiteDef{
	"laravel": {
		Key:          "laravel",
		Runtime:      "php",
		UpstreamPort: 8080,
		XdebugTarget: "/usr/local/etc/php/conf.d/99-xdebug.ini",
		// Keep Composer's vendor/ off the slow bind mount: an empty named volume
		// shadows the host vendor/, and Hull's /etc/entrypoint.d composer-install
		// script (see SeedsComposer) fills it before PHP-FPM serves.
		NamedVolumes: []NamedVolume{{Name: "vendor", Path: "/var/www/html/vendor"}},
	},
	"plain": {
		Key:          "plain",
		Runtime:      "php",
		UpstreamPort: 8080,
		XdebugTarget: "/usr/local/etc/php/conf.d/99-xdebug.ini",
		ExtraEnv: []string{
			"NGINX_WEBROOT=/var/www/html",
			"WEB_DOCUMENT_ROOT=/var/www/html",
		},
	},
	"wordpress": {
		Key:          "wordpress",
		Runtime:      "php",
		UpstreamPort: 80,
		XdebugTarget: "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini",
		RequiredDB:   []string{"mariadb", "mysql"},
	},
	// Static site: serve files straight from the project directory with nginx.
	// No runtime, no build, no database; edits are live over the bind mount.
	"static": {
		Key:          "static",
		Runtime:      "static",
		UpstreamPort: 80,
		BaseImage:    "nginx:alpine",
		Mount:        "/usr/share/nginx/html",
	},
	// Plain Python: a python:slim container with your code at /app and a venv on
	// a named volume (kept off the slow bind mount). It installs requirements.txt
	// and runs app.py; use `hull python` / `hull pip` to run scripts and manage
	// packages. No web framework is assumed (bring your own, or serve stdlib).
	"python": {
		Key:          "python",
		Runtime:      "python",
		UpstreamPort: 8000,
		Mount:        "/app",
		Workdir:      "/app",
		ExtraEnv: []string{
			"PYTHONUNBUFFERED=1",
			"VIRTUAL_ENV=/opt/venv",
			"PATH=/opt/venv/bin:/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/bin:/sbin",
			"PORT=8000",
		},
		Command:      `sh -c '[ -d /opt/venv/bin ] || python -m venv /opt/venv; pip install -q -r requirements.txt 2>/dev/null || true; exec python app.py'`,
		NamedVolumes: []NamedVolume{{Name: "venv", Path: "/opt/venv"}, {Name: "pip_cache", Path: "/root/.cache/pip"}},
	},
	// Node: a node:slim container with your code at /app and node_modules on a
	// named volume (off the bind mount). Installs package.json deps on boot and
	// runs server.js. Use `hull node` to run scripts; add deps to package.json.
	"node": {
		Key:          "node",
		Runtime:      "node",
		UpstreamPort: 8000,
		Mount:        "/app",
		Workdir:      "/app",
		ExtraEnv:     []string{"PORT=8000", "NODE_ENV=development"},
		Command:      `sh -c '[ -f package.json ] && npm install --no-audit --no-fund --loglevel=error 2>/dev/null; exec node server.js'`,
		NamedVolumes: []NamedVolume{{Name: "node_modules", Path: "/app/node_modules"}},
	},
	// Go: a golang container that rebuilds and reruns on change via air, with the
	// module and build caches on named volumes (off the bind mount). Use
	// `hull go` for the toolchain (build/run/test/mod).
	"go": {
		Key:          "go",
		Runtime:      "go",
		UpstreamPort: 8080,
		Mount:        "/app",
		Workdir:      "/app",
		ExtraEnv:     []string{"PORT=8080"},
		Command:      `sh -c 'command -v air >/dev/null 2>&1 || go install github.com/air-verse/air@latest; exec air'`,
		NamedVolumes: []NamedVolume{{Name: "go_mod", Path: "/go/pkg/mod"}, {Name: "go_build", Path: "/root/.cache/go-build"}, {Name: "go_bin", Path: "/go/bin"}},
	},
}

// IsPHP reports whether the template runs on a PHP image (laravel, plain,
// wordpress). Empty runtime is treated as PHP for back-compat.
func (d SiteDef) IsPHP() bool { return d.Runtime == "php" || d.Runtime == "" }

// ServersideUp reports whether the template runs on a serversideup/php image,
// which honours PUID/PGID to match the container user to the host (needed for
// writable bind mounts on native Linux Docker). WordPress uses the upstream
// wordpress image, and non-PHP runtimes are not serversideup at all.
func (d SiteDef) ServersideUp() bool { return d.IsPHP() && d.Key != "wordpress" }

// SeedsComposer reports whether this template keeps vendor/ on a named volume
// that a boot-time `composer install` must populate. True for any serversideup
// PHP template carrying a "vendor" named volume (laravel today). It gates the
// /etc/entrypoint.d composer-install mount in the renderer, so the empty volume
// is filled before the web server serves and self-heals after `hull reset`.
func (d SiteDef) SeedsComposer() bool {
	if !d.ServersideUp() {
		return false
	}
	for _, nv := range d.NamedVolumes {
		if nv.Name == "vendor" {
			return true
		}
	}
	return false
}

// MountTarget is where the project directory is bind-mounted in the container,
// defaulting to the PHP webroot.
func (d SiteDef) MountTarget() string {
	if d.Mount != "" {
		return d.Mount
	}
	return "/var/www/html"
}

// PHPConfDir is where every PHP image Hull uses loads extra ini files: both
// serversideup/php and the upstream wordpress image are built on the official
// php image, whose scan dir is this path. Hull mounts its opcache.ini here, and
// a custom `php_tune` app container is assumed to use the same layout.
const PHPConfDir = "/usr/local/etc/php/conf.d"

// Site returns the built-in site template for key.
func Site(key string) (SiteDef, bool) {
	d, ok := sites[key]
	return d, ok
}

// SiteKeys returns the built-in template keys, sorted.
func SiteKeys() []string {
	keys := make([]string, 0, len(sites))
	for k := range sites {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Image returns the web image for the template. For PHP templates, php applies
// to laravel/plain and version pins the wordpress image tag. For a non-PHP
// runtime it is the template's BaseImage.
func (d SiteDef) Image(php, version string) string {
	if !d.IsPHP() {
		switch d.Runtime {
		case "python":
			v := version
			if v == "" {
				v = DefaultPython
			}
			return "python:" + v + "-slim"
		case "node":
			v := version
			if v == "" {
				v = DefaultNode
			}
			return "node:" + v + "-slim"
		case "go":
			v := version
			if v == "" {
				v = DefaultGo
			}
			return "golang:" + v
		default:
			return d.BaseImage
		}
	}
	if d.Key == "wordpress" {
		if version == "" {
			version = "latest"
		}
		return "wordpress:" + version
	}
	if php == "" {
		php = DefaultPHP
	}
	return fmt.Sprintf("serversideup/php:%s-fpm-nginx", php)
}

// EngineDef describes a built-in service engine. Data-driven: adding an
// engine is a map entry, no switch edits.
type EngineDef struct {
	Name           string
	Category       string // database | cache | search | storage | mail | tool
	DefaultVersion string
	// imageRepo is the image without tag; the tag is the version.
	imageRepo string
	// imageTagSuffix is appended to the version in the tag (e.g. "-alpine").
	imageTagSuffix string
	// defaultTag is the image tag when no version is given AND
	// DefaultVersion is empty (lets non-db services keep clean instance
	// names while still pinning an image, e.g. redis "" -> redis:alpine).
	defaultTag string
	// DataPath is the in-container directory persisted to a named volume.
	DataPath string
	// Command overrides the container command (e.g. minio "server").
	Command string
	// HealthTest is the compose healthcheck test command for a database engine
	// (the vendor-recommended readiness probe), empty for engines Hull does not
	// gate a dependent on. Lets a site app wait on condition: service_healthy.
	HealthTest []string
	// fixedEnv is instance environment that does not depend on a database.
	fixedEnv []string
	// JoinsCaddy: the instance joins the caddy network (reachable by other
	// shared services and the router).
	JoinsCaddy bool
	IsDatabase bool
	// containerPort is the primary in-network port (string for env wiring).
	containerPort string
	// UIPort is the container port of an embedded web UI (0 = none).
	UIPort int
	// UISubdomain serves that UI at <sub>.<tld> through the daemon router.
	UISubdomain string
	// HostPortBase: instances publish their primary port on a STABLE
	// loopback host port scanned upward from here (0 = no primary publish).
	HostPortBase int
}

var engines = map[string]EngineDef{
	"postgres": {Name: "postgres", Category: "database", DefaultVersion: "16", imageRepo: "postgres", imageTagSuffix: "-alpine", DataPath: "/var/lib/postgresql/data", JoinsCaddy: true, IsDatabase: true, containerPort: "5432", HostPortBase: 54320,
		fixedEnv:   []string{"POSTGRES_HOST_AUTH_METHOD=trust", "POSTGRES_USER=postgres"},
		HealthTest: []string{"CMD-SHELL", "pg_isready -U postgres"}},
	"mysql": {Name: "mysql", Category: "database", DefaultVersion: "8.0", imageRepo: "mysql", DataPath: "/var/lib/mysql", JoinsCaddy: true, IsDatabase: true, containerPort: "3306", HostPortBase: 53360,
		fixedEnv:   []string{"MYSQL_ALLOW_EMPTY_PASSWORD=yes"},
		HealthTest: []string{"CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -u root --silent"}},
	"mariadb": {Name: "mariadb", Category: "database", DefaultVersion: "lts", imageRepo: "mariadb", DataPath: "/var/lib/mysql", JoinsCaddy: true, IsDatabase: true, containerPort: "3306", HostPortBase: 53390,
		fixedEnv:   []string{"MYSQL_ALLOW_EMPTY_PASSWORD=yes"},
		HealthTest: []string{"CMD-SHELL", "healthcheck.sh --connect --innodb_initialized"}},
	"redis":   {Name: "redis", Category: "cache", imageRepo: "redis", defaultTag: "alpine", DataPath: "/data", containerPort: "6379", HostPortBase: 56379},
	"mailpit": {Name: "mailpit", Category: "mail", imageRepo: "axllent/mailpit", defaultTag: "latest", DataPath: "/data", JoinsCaddy: true, containerPort: "1025", UIPort: 8025, UISubdomain: "mail", HostPortBase: 52525, fixedEnv: []string{"MP_DATABASE=/data/mailpit.db"}},
	"adminer": {Name: "adminer", Category: "tool", imageRepo: "adminer", defaultTag: "latest", JoinsCaddy: true, UIPort: 8080, UISubdomain: "db"},
	// Redis viewer: RedisInsight has a runtime "add database" UI (like
	// Adminer for SQL) and persists connections, so it needs no per-instance
	// wiring. On the caddy network it can reach hull-redis-* by container name.
	"redisinsight": {Name: "redisinsight", Category: "tool", imageRepo: "redis/redisinsight", defaultTag: "latest", DataPath: "/data", JoinsCaddy: true, UIPort: 5540, UISubdomain: "redis"},

	// Herd-parity additions (Wave H). Non-db engines keep clean instance
	// names (empty DefaultVersion) but pin an image via defaultTag.
	"memcached": {Name: "memcached", Category: "cache", imageRepo: "memcached", defaultTag: "alpine", containerPort: "11211", HostPortBase: 51121},
	"meilisearch": {Name: "meilisearch", Category: "search", imageRepo: "getmeili/meilisearch", defaultTag: "v1.11", DataPath: "/meili_data", JoinsCaddy: true,
		containerPort: "7700", UIPort: 7700, UISubdomain: "search", HostPortBase: 57700,
		fixedEnv: []string{"MEILI_ENV=development", "MEILI_MASTER_KEY=hullMasterKey", "MEILI_NO_ANALYTICS=true"}},
	"typesense": {Name: "typesense", Category: "search", imageRepo: "typesense/typesense", defaultTag: "27.1", DataPath: "/data", JoinsCaddy: true,
		containerPort: "8108", HostPortBase: 58108, Command: "--data-dir /data --api-key=hullTypesenseKey --enable-cors"},
	"minio": {Name: "minio", Category: "storage", imageRepo: "minio/minio", defaultTag: "latest", DataPath: "/data", JoinsCaddy: true,
		containerPort: "9000", UIPort: 9001, UISubdomain: "storage", HostPortBase: 59000, Command: "server /data --console-address :9001",
		fixedEnv: []string{"MINIO_ROOT_USER=hull", "MINIO_ROOT_PASSWORD=hullsecret"}},
}

// SitePHPRepo is the Docker Hub repo Hull's PHP site image comes from.
const SitePHPRepo = "serversideup/php"

// PHPVersionRepo drives the live PHP-version picker. Hull's site image is
// serversideup/php, but that repo's recent tags are mostly beta builds;
// the official php image exposes clean X.Y tags for the same minors.
const PHPVersionRepo = "php"

// Engine returns the built-in engine definition for name.
func Engine(name string) (EngineDef, bool) {
	e, ok := engines[name]
	return e, ok
}

// Repo is the Docker Hub repository for the engine's image (used for live
// version lookups). Empty when the engine has no published versions.
func (e EngineDef) Repo() string { return e.imageRepo }

// EngineKeys returns the built-in engine names, sorted.
func EngineKeys() []string {
	keys := make([]string, 0, len(engines))
	for k := range engines {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Image returns the container image for the engine at the given version
// (empty means the engine default).
func (e EngineDef) Image(version string) string {
	if version == "" {
		version = e.DefaultVersion
	}
	if e.imageRepo == "" {
		return ""
	}
	if version == "" {
		tag := e.defaultTag
		if tag == "" {
			tag = "latest"
		}
		return e.imageRepo + ":" + tag
	}
	return e.imageRepo + ":" + version + e.imageTagSuffix
}

// Env returns the engine's container environment (KEY=value pairs) for the
// given database name. An empty database omits the create-database variable
// , shared instances create databases per linked project instead.
func (e EngineDef) Env(database string) []string {
	env := append([]string(nil), e.fixedEnv...)
	if e.IsDatabase && database != "" {
		switch e.Name {
		case "postgres":
			env = append([]string{"POSTGRES_DB=" + database}, env...)
		case "mysql", "mariadb":
			env = append(env, "MYSQL_DATABASE="+database)
		}
	}
	return env
}

// InstanceName names a shared service instance for an engine+version, e.g.
// "postgres-16", "mariadb-lts", "redis". Empty version means the default.
func InstanceName(engine, version string) string {
	e, ok := engines[engine]
	if !ok {
		return engine
	}
	if version == "" {
		version = e.DefaultVersion
	}
	if version == "" {
		return engine
	}
	return engine + "-" + version
}

// InstanceContainerName is the docker container name (and in-network
// hostname) of a shared instance, e.g. "hull-postgres-16".
func InstanceContainerName(engine, version string) string {
	return "hull-" + InstanceName(engine, version)
}

// DefaultPort is the engine's in-network port, used when wiring project
// env files to shared instances and publishing the stable host port.
func (e EngineDef) DefaultPort() string {
	return e.containerPort
}
