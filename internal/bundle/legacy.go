package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LegacyInfo is what can be recovered from a bash-Hull (v1) compose file.
type LegacyInfo struct {
	Detection
	DBVersion string
	// Host is the full caddy hostname (e.g. "blog.test"), if labeled.
	Host string
	// ComposeFile is the legacy file name found (compose.yaml, ...).
	ComposeFile string
}

var legacyComposeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// DetectLegacy reconstructs project facts from a v1 compose file — the
// basis of `hull migrate`. v1 files used map-form environment and labels.
func DetectLegacy(dir string) (LegacyInfo, error) {
	var (
		data []byte
		file string
		err  error
	)
	for _, name := range legacyComposeNames {
		if data, err = os.ReadFile(filepath.Join(dir, name)); err == nil {
			file = name
			break
		}
	}
	if file == "" {
		return LegacyInfo{}, fmt.Errorf("no compose file found in %s", dir)
	}

	var doc struct {
		Services map[string]struct {
			Image       string         `yaml:"image"`
			Environment map[string]any `yaml:"environment"`
			Labels      map[string]any `yaml:"labels"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return LegacyInfo{}, fmt.Errorf("parsing %s: %w", file, err)
	}

	info := LegacyInfo{ComposeFile: file}
	for _, svc := range doc.Services {
		image := svc.Image
		switch {
		case strings.HasPrefix(image, "serversideup/php:"):
			info.Template = "laravel"
			if _, ok := svc.Environment["WEB_DOCUMENT_ROOT"]; ok {
				info.Template = "plain"
			}
			info.PHP = phpFromImage(image)
			info.Host = caddyHost(svc.Labels)
		case strings.HasPrefix(image, "wordpress:"):
			info.Template = "wordpress"
			info.Host = caddyHost(svc.Labels)
			if v, ok := svc.Environment["WORDPRESS_DB_NAME"]; ok {
				info.Database = fmt.Sprint(v)
			}
		case strings.HasPrefix(image, "postgres:"):
			info.DB = "postgres"
			info.DBVersion = versionFromTag(image, "-alpine")
			if v, ok := svc.Environment["POSTGRES_DB"]; ok {
				info.Database = fmt.Sprint(v)
			}
		case strings.HasPrefix(image, "mysql:"):
			info.DB = "mysql"
			info.DBVersion = versionFromTag(image, "")
			if v, ok := svc.Environment["MYSQL_DATABASE"]; ok && info.Database == "" {
				info.Database = fmt.Sprint(v)
			}
		case strings.HasPrefix(image, "mariadb:"):
			info.DB = "mariadb"
			info.DBVersion = versionFromTag(image, "")
			if v, ok := svc.Environment["MYSQL_DATABASE"]; ok && info.Database == "" {
				info.Database = fmt.Sprint(v)
			}
		case strings.HasPrefix(image, "redis:"):
			info.Redis = true
		}
	}
	if info.Template == "" {
		return info, fmt.Errorf("could not recognize a Hull v1 web service in %s", file)
	}
	if info.Database == "hull_db" {
		info.Database = "" // v1 default; let v2 derive from the project name
	}
	return info, nil
}

// phpFromImage extracts "8.3" from "serversideup/php:8.3-fpm-nginx".
func phpFromImage(image string) string {
	_, tag, ok := strings.Cut(image, ":")
	if !ok {
		return ""
	}
	version, _, _ := strings.Cut(tag, "-")
	if version == "8.4" {
		return "" // default; keep the manifest minimal
	}
	return version
}

// versionFromTag extracts "16" from "postgres:16-alpine" / "8.0" from
// "mysql:8.0". Engine defaults collapse to "".
func versionFromTag(image, stripSuffix string) string {
	_, tag, ok := strings.Cut(image, ":")
	if !ok {
		return ""
	}
	if stripSuffix != "" {
		tag = strings.TrimSuffix(tag, stripSuffix)
	}
	switch tag {
	case "16", "8.0", "lts", "latest", "alpine":
		return "" // engine default
	}
	return tag
}

// caddyHost pulls the routed hostname from v1's caddy label.
func caddyHost(labels map[string]any) string {
	if v, ok := labels["caddy"]; ok {
		return fmt.Sprint(v)
	}
	return ""
}
