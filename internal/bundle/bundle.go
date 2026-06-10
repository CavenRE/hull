package bundle

import (
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MetaSchema is the bundle format version. Bump on incompatible changes;
// importers must refuse newer schemas.
const MetaSchema = 1

// Meta is bundle/manifest.json.
type Meta struct {
	BundleSchema int               `json:"bundle_schema"`
	HullVersion  string            `json:"hull_version"`
	CreatedAt    time.Time         `json:"created_at"`
	ProjectYAML  string            `json:"project_yaml"`       // hull.yaml content
	Dumps        map[string]string `json:"dumps,omitempty"`    // service key -> archive path
	EnvPath      string            `json:"env_path,omitempty"` // archive path of .env, if included
	EnvStripped  []string          `json:"env_stripped,omitempty"`
}

// DefaultExcludes are directory names skipped during export (regenerable
// or machine-local).
var DefaultExcludes = []string{".git", "node_modules", "vendor"}

// secretKeyRE marks env keys whose values are stripped unless the user
// opts in to a full env export.
var secretKeyRE = regexp.MustCompile(`(?i)(secret|password|token|_key$|^key_|apikey)`)

// ExportOptions configures WriteBundle.
type ExportOptions struct {
	ProjectDir  string
	ProjectYAML string
	HullVersion string
	IncludeEnv  bool // include secret values verbatim
	KeepVendor  bool // do not exclude vendor/node_modules
	// DumpDB streams a plain-SQL dump of the given service key into w.
	// Nil means no database dumps.
	DumpDB   func(key string, w io.Writer) error
	DumpKeys []string
}

// WriteBundle writes a hull-bundle zip to w.
func WriteBundle(w io.Writer, opts ExportOptions) (*Meta, error) {
	zw := zip.NewWriter(w)
	meta := &Meta{
		BundleSchema: MetaSchema,
		HullVersion:  opts.HullVersion,
		CreatedAt:    time.Now().UTC(),
		ProjectYAML:  opts.ProjectYAML,
		Dumps:        map[string]string{},
	}

	excludes := map[string]bool{".git": true}
	if !opts.KeepVendor {
		excludes["node_modules"] = true
		excludes["vendor"] = true
	}

	err := filepath.WalkDir(opts.ProjectDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(opts.ProjectDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := d.Name()
		if d.IsDir() && excludes[name] {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		// compose.yaml is a generated artifact; .env is handled separately.
		if rel == "compose.yaml" || rel == ".env" || strings.HasSuffix(name, "-bundle.zip") {
			return nil
		}
		return addFile(zw, p, path.Join("app", filepath.ToSlash(rel)))
	})
	if err != nil {
		return nil, err
	}

	for _, key := range opts.DumpKeys {
		if opts.DumpDB == nil {
			break
		}
		archivePath := path.Join("db", key+".sql.gz")
		entry, err := zw.Create(archivePath)
		if err != nil {
			return nil, err
		}
		gz := gzip.NewWriter(entry)
		if err := opts.DumpDB(key, gz); err != nil {
			return nil, fmt.Errorf("dumping %s: %w", key, err)
		}
		if err := gz.Close(); err != nil {
			return nil, err
		}
		meta.Dumps[key] = archivePath
	}

	if envData, err := os.ReadFile(filepath.Join(opts.ProjectDir, ".env")); err == nil {
		content := string(envData)
		if !opts.IncludeEnv {
			content, meta.EnvStripped = stripSecrets(content)
		}
		entry, err := zw.Create("env/.env")
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(entry, content); err != nil {
			return nil, err
		}
		meta.EnvPath = "env/.env"
	}

	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	entry, err := zw.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := entry.Write(metaJSON); err != nil {
		return nil, err
	}
	return meta, zw.Close()
}

func addFile(zw *zip.Writer, src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	entry, err := zw.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, f)
	return err
}

// stripSecrets blanks values of secret-looking keys, returning the new
// content and the sorted list of stripped keys.
func stripSecrets(content string) (string, []string) {
	lines := strings.Split(content, "\n")
	var stripped []string
	for i, line := range lines {
		trimmed := strings.TrimSuffix(line, "\r")
		key, val, found := strings.Cut(trimmed, "=")
		if !found || strings.HasPrefix(trimmed, "#") || val == "" {
			continue
		}
		if secretKeyRE.MatchString(key) {
			lines[i] = key + "="
			stripped = append(stripped, key)
		}
	}
	sort.Strings(stripped)
	return strings.Join(lines, "\n"), stripped
}

// ReadMeta extracts manifest.json from a bundle zip without unpacking.
func ReadMeta(zipPath string) (*Meta, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		var meta Meta
		if err := json.NewDecoder(rc).Decode(&meta); err != nil {
			return nil, err
		}
		if meta.BundleSchema > MetaSchema {
			return nil, fmt.Errorf("bundle schema %d is newer than this Hull understands (max %d)", meta.BundleSchema, MetaSchema)
		}
		return &meta, nil
	}
	return nil, fmt.Errorf("not a hull bundle (no manifest.json)")
}

// Extract unpacks a bundle: app/ into projectDir, env/.env into projectDir
// (skipped if one exists), db dumps into projectDir as <key>.sql.gz for the
// standard dump-import flow. Zip-slip safe.
func Extract(zipPath, projectDir string) (*Meta, error) {
	meta, err := ReadMeta(zipPath)
	if err != nil {
		return nil, err
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		var dest string
		switch {
		case strings.HasPrefix(f.Name, "app/"):
			dest = strings.TrimPrefix(f.Name, "app/")
		case f.Name == meta.EnvPath && meta.EnvPath != "":
			if _, err := os.Stat(filepath.Join(projectDir, ".env")); err == nil {
				continue // never clobber an existing .env
			}
			dest = ".env"
		case strings.HasPrefix(f.Name, "db/"):
			dest = path.Base(f.Name)
		default:
			continue
		}
		if dest == "" || f.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(projectDir, filepath.FromSlash(dest))
		cleanRoot := filepath.Clean(projectDir) + string(filepath.Separator)
		if !strings.HasPrefix(filepath.Clean(target)+string(filepath.Separator), cleanRoot) {
			return nil, fmt.Errorf("bundle entry escapes project dir: %s", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := extractFile(f, target); err != nil {
			return nil, err
		}
	}
	return meta, nil
}

func extractFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, rc)
	return err
}

// StrippedEnvHint formats a user warning about stripped secrets.
func StrippedEnvHint(meta *Meta) string {
	if len(meta.EnvStripped) == 0 {
		return ""
	}
	return "Stripped secret values (refill after import): " + strings.Join(meta.EnvStripped, ", ")
}
