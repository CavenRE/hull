package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CavenRE/hull/internal/envfile"
)

// Detection is what auto-discovery learned about an existing project —
// the Go port of v1's import sniffing.
type Detection struct {
	Template string // laravel | wordpress | plain
	PHP      string // "" = default
	DB       string // engine name, "" = none
	Database string // database name, "" = derive from project name
	Redis    bool
}

// Detect inspects a project directory.
func Detect(dir string) Detection {
	d := Detection{Template: detectTemplate(dir)}
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
