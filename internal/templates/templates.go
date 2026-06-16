package templates

import (
	"fmt"
	"sort"
)

// DefaultPHP is the PHP version used when a manifest does not pin one.
const DefaultPHP = "8.4"

// SiteDef describes a built-in site template: the web container Hull
// generates for it and how requests reach it. Ported from v1's
// templates/base/*.yaml (fixing plain's missing default network).
type SiteDef struct {
	Key          string
	UpstreamPort int
	// XdebugTarget is where the shared xdebug.ini mounts inside the container.
	XdebugTarget string
	// ExtraEnv is template-fixed environment (KEY=value pairs).
	ExtraEnv []string
	// RequiredDB lists acceptable database engines when the template cannot
	// run without one (e.g. wordpress).
	RequiredDB []string
}

var sites = map[string]SiteDef{
	"laravel": {
		Key:          "laravel",
		UpstreamPort: 8080,
		XdebugTarget: "/usr/local/etc/php/conf.d/99-xdebug.ini",
	},
	"plain": {
		Key:          "plain",
		UpstreamPort: 8080,
		XdebugTarget: "/usr/local/etc/php/conf.d/99-xdebug.ini",
		ExtraEnv: []string{
			"NGINX_WEBROOT=/var/www/html",
			"WEB_DOCUMENT_ROOT=/var/www/html",
		},
	},
	"wordpress": {
		Key:          "wordpress",
		UpstreamPort: 80,
		XdebugTarget: "/usr/local/etc/php/conf.d/docker-php-ext-xdebug.ini",
		RequiredDB:   []string{"mariadb", "mysql"},
	},
}

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

// Image returns the web image for the template. php applies to
// laravel/plain; version pins the wordpress image tag.
func (d SiteDef) Image(php, version string) string {
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
		fixedEnv: []string{"POSTGRES_HOST_AUTH_METHOD=trust", "POSTGRES_USER=postgres"}},
	"mysql": {Name: "mysql", Category: "database", DefaultVersion: "8.0", imageRepo: "mysql", DataPath: "/var/lib/mysql", JoinsCaddy: true, IsDatabase: true, containerPort: "3306", HostPortBase: 53360,
		fixedEnv: []string{"MYSQL_ALLOW_EMPTY_PASSWORD=yes"}},
	"mariadb": {Name: "mariadb", Category: "database", DefaultVersion: "lts", imageRepo: "mariadb", DataPath: "/var/lib/mysql", JoinsCaddy: true, IsDatabase: true, containerPort: "3306", HostPortBase: 53390,
		fixedEnv: []string{"MYSQL_ALLOW_EMPTY_PASSWORD=yes"}},
	"redis":   {Name: "redis", Category: "cache", imageRepo: "redis", defaultTag: "alpine", DataPath: "/data", containerPort: "6379", HostPortBase: 56379},
	"mailpit": {Name: "mailpit", Category: "mail", imageRepo: "axllent/mailpit", defaultTag: "latest", DataPath: "/data", JoinsCaddy: true, containerPort: "1025", UIPort: 8025, UISubdomain: "mail", HostPortBase: 52525, fixedEnv: []string{"MP_DATABASE=/data/mailpit.db"}},
	"adminer": {Name: "adminer", Category: "tool", imageRepo: "adminer", defaultTag: "latest", JoinsCaddy: true, UIPort: 8080, UISubdomain: "db"},

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
// — shared instances create databases per linked project instead.
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
