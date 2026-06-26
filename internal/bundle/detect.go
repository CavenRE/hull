package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CavenRE/hull/internal/envfile"
)

// Detection is what auto-discovery learned about an existing project ,
// the Go port of v1's import sniffing.
type Detection struct {
	Template string // laravel | wordpress | plain (PHP site template)
	PHP      string // "" = default
	DB       string // engine name, "" = none
	Database string // database name, "" = derive from project name
	Redis    bool
	// Extras are additional shared-service engines discovered in .env
	// (mailpit, meilisearch, typesense, memcached, minio) , wired the same
	// way redis/db are, so an imported app comes up fully connected.
	Extras []string
	// Kind is the broader project kind for non-PHP projects too:
	// laravel | wordpress | plain | python | node | go | docker | static.
	// PHP kinds map to a site Template; the rest are container/cluster apps.
	Kind string
}

// PHPKind reports whether a detected kind is a PHP site Hull can import as-is.
func (d Detection) PHPKind() bool {
	return d.Kind == "laravel" || d.Kind == "wordpress" || d.Kind == "plain"
}

// Detect inspects a project directory.
func Detect(dir string) Detection {
	d := Detection{Template: detectTemplate(dir), Kind: DetectKind(dir)}
	d.PHP = detectPHP(dir)
	switch d.Template {
	case "laravel", "plain":
		if env, err := os.ReadFile(filepath.Join(dir, ".env")); err == nil {
			content := string(env)
			d.DB = detectDBFromEnv(content)
			d.Database, _ = envfile.Get(content, "DB_DATABASE")
			d.Database = strings.Trim(strings.TrimSpace(d.Database), `"'`)
			if redisHost, ok := envfile.Get(content, "REDIS_HOST"); ok && redisHost != "" {
				d.Redis = true
			} else if store, ok := envfile.Get(content, "CACHE_STORE"); ok && store == "redis" {
				d.Redis = true
			}
			d.Extras = detectExtras(content)
		}
		if d.DB == "" && d.Template == "laravel" {
			d.DB = "postgres" // smart default, as v1
		}
	case "wordpress":
		d.DB = "mariadb"
		if cfg, err := os.ReadFile(filepath.Join(dir, "wp-config.php")); err == nil {
			d.Database = wpDefine(string(cfg), "DB_NAME")
		}
	}
	return d
}

func detectTemplate(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if exists("artisan") {
		return "laravel"
	}
	if data, err := os.ReadFile(filepath.Join(dir, "composer.json")); err == nil &&
		strings.Contains(string(data), "laravel/framework") {
		return "laravel"
	}
	if exists("wp-config.php") || exists("wp-config-sample.php") || exists("wp-includes") {
		return "wordpress"
	}
	return "plain"
}

// DetectKind classifies a project by the files it ships , used so the GUI
// stops defaulting everything to Laravel. PHP frameworks win first; then
// language/runtime markers; then a generic "static"/"plain" fallback.
func DetectKind(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("artisan"):
		return "laravel"
	case exists("wp-config.php") || exists("wp-config-sample.php") || exists("wp-includes"):
		return "wordpress"
	}
	if data, err := os.ReadFile(filepath.Join(dir, "composer.json")); err == nil {
		if strings.Contains(string(data), "laravel/framework") {
			return "laravel"
		}
		return "plain" // PHP project without a framework
	}
	switch {
	case exists("manage.py") || exists("requirements.txt") || exists("pyproject.toml") || exists("Pipfile"):
		return "python"
	case exists("package.json"):
		return "node"
	case exists("go.mod"):
		return "go"
	case exists("Dockerfile") || exists("docker-compose.yml") || exists("docker-compose.yaml") || exists("compose.yaml") || exists("compose.yml"):
		return "docker"
	case exists("index.php"):
		return "plain"
	case exists("index.html"):
		return "static"
	}
	return "plain"
}

// detectPHP maps a composer.json php requirement onto a supported version.
func detectPHP(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return ""
	}
	var composer struct {
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		return ""
	}
	req := composer.Require["php"]
	// Highest mentioned version wins, matching v1's cascade.
	for _, v := range []string{"8.4", "8.3", "8.2", "8.1"} {
		if strings.Contains(req, v) {
			return v
		}
	}
	return ""
}

// detectExtras finds shared services beyond db/redis from a Laravel .env,
// conservatively (only on clear markers) so imports don't provision junk.
func detectExtras(content string) []string {
	get := func(k string) string {
		v, _ := envfile.Get(content, k)
		return strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`))
	}
	var out []string
	has := func(e string) bool {
		for _, x := range out {
			if x == e {
				return true
			}
		}
		return false
	}
	add := func(e string) {
		if !has(e) {
			out = append(out, e)
		}
	}

	// Mail catcher (mailpit/mailhog) when SMTP points at one.
	if get("MAIL_MAILER") == "smtp" {
		if h := get("MAIL_HOST"); strings.Contains(h, "mailpit") || strings.Contains(h, "mailhog") {
			add("mailpit")
		}
	}
	// Full-text search via Laravel Scout.
	switch get("SCOUT_DRIVER") {
	case "meilisearch":
		add("meilisearch")
	case "typesense":
		add("typesense")
	}
	if get("MEILISEARCH_HOST") != "" {
		add("meilisearch")
	}
	if get("TYPESENSE_HOST") != "" {
		add("typesense")
	}
	// Memcached cache.
	if get("CACHE_STORE") == "memcached" || get("MEMCACHED_HOST") != "" {
		add("memcached")
	}
	// MinIO / S3-compatible object storage (path-style endpoint is the tell).
	if get("FILESYSTEM_DISK") == "s3" && (get("AWS_ENDPOINT") != "" || get("AWS_USE_PATH_STYLE_ENDPOINT") == "true") {
		add("minio")
	}
	return out
}

func detectDBFromEnv(content string) string {
	conn, _ := envfile.Get(content, "DB_CONNECTION")
	switch strings.TrimSpace(conn) {
	case "pgsql":
		return "postgres"
	case "mysql":
		return "mysql"
	case "mariadb":
		return "mariadb"
	case "sqlite":
		return ""
	}
	return ""
}

var wpDefineRE = regexp.MustCompile(`define\(\s*['"]([A-Z_]+)['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)

// wpDefine extracts a define() value from wp-config.php content.
func wpDefine(content, name string) string {
	for _, m := range wpDefineRE.FindAllStringSubmatch(content, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

// FindDumps lists database dump candidates in a directory (v1 scanned
// *.sql, *.sql.gz, *.zip), excluding Hull bundles.
func FindDumps(dir string) []string {
	var dumps []string
	for _, pattern := range []string{"*.sql", "*.sql.gz", "*.zip"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, m := range matches {
			if strings.HasSuffix(m, "-bundle.zip") {
				continue
			}
			dumps = append(dumps, m)
		}
	}
	return dumps
}
