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

// EngineDef describes a built-in service engine, ported from v1's
// templates/services/*.yaml.
type EngineDef struct {
	Name           string
	DefaultVersion string
	// DataPath is the in-container directory persisted to a named volume.
	DataPath string
	// JoinsCaddy: database engines join the caddy network so the global
	// Adminer can reach them (v1 behavior).
	JoinsCaddy bool
	IsDatabase bool
}

var engines = map[string]EngineDef{
	"postgres": {Name: "postgres", DefaultVersion: "16", DataPath: "/var/lib/postgresql/data", JoinsCaddy: true, IsDatabase: true},
	"mysql":    {Name: "mysql", DefaultVersion: "8.0", DataPath: "/var/lib/mysql", JoinsCaddy: true, IsDatabase: true},
	"mariadb":  {Name: "mariadb", DefaultVersion: "lts", DataPath: "/var/lib/mysql", JoinsCaddy: true, IsDatabase: true},
	"redis":    {Name: "redis", DataPath: "/data"},
}

// Engine returns the built-in engine definition for name.
func Engine(name string) (EngineDef, bool) {
	e, ok := engines[name]
	return e, ok
}

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
	switch e.Name {
	case "postgres":
		return "postgres:" + version + "-alpine"
	case "mysql":
		return "mysql:" + version
	case "mariadb":
		return "mariadb:" + version
	case "redis":
		if version == "" {
			return "redis:alpine"
		}
		return "redis:" + version + "-alpine"
	}
	return ""
}

// Env returns the engine's container environment (KEY=value pairs, sorted)
// for the given database name.
func (e EngineDef) Env(database string) []string {
	switch e.Name {
	case "postgres":
		return []string{
			"POSTGRES_DB=" + database,
			"POSTGRES_HOST_AUTH_METHOD=trust",
			"POSTGRES_USER=postgres",
		}
	case "mysql", "mariadb":
		return []string{
			"MYSQL_ALLOW_EMPTY_PASSWORD=yes",
			"MYSQL_DATABASE=" + database,
		}
	}
	return nil
}
