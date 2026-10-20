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
	// Skipped lists project files left out: symbolic links and other
	// non-regular files, Hull's *.hull-backup copies, and env files that are
	// not templates (unless the export kept secrets).
	Skipped []string `json:"skipped,omitempty"`
}

// secretKeyRE marks env keys whose values are stripped unless the user
// opts in to a full env export.
var secretKeyRE = regexp.MustCompile(`(?i)(secret|passw|(^|_)pass|token|_key$|^key_|apikey|private|credential)`)

// urlCredRE finds a password inside a URL value (scheme://user:pass@host).
var urlCredRE = regexp.MustCompile(`://[^/\s:@]*:[^@\s/]+@`)

// ExportOptions configures WriteBundle.
type ExportOptions struct {
	ProjectDir  string
	ProjectYAML string
	HullVersion string
	IncludeEnv  bool // include secret values verbatim
	KeepVendor  bool // do not exclude vendor/node_modules
	KeepCompose bool // compose.yaml is the user's own file (clusters), not generated
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

	// vendor and node_modules are regenerable only at the project root; a
	// nested one (public/vendor holds published package assets) is real content.
	excludeAtRoot := map[string]bool{}
	if !opts.KeepVendor {
		excludeAtRoot["node_modules"] = true
		excludeAtRoot["vendor"] = true
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
		if d.IsDir() {
			if name == ".git" || excludeAtRoot[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		// A site's compose.yaml is a generated artifact; .env is handled separately.
		if (rel == "compose.yaml" && !opts.KeepCompose) || rel == ".env" || strings.HasSuffix(name, "-bundle.zip") {
			return nil
		}
		// Only regular files: a symbolic link can pull in a file from outside
		// the project (a key under ~/.ssh), and a FIFO would hang the export.
		if !d.Type().IsRegular() || strings.HasSuffix(name, ".hull-backup") || (!opts.IncludeEnv && envFile(name)) {
			meta.Skipped = append(meta.Skipped, filepath.ToSlash(rel))
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

	envPath := filepath.Join(opts.ProjectDir, ".env")
	if fi, err := os.Lstat(envPath); err == nil && !fi.Mode().IsRegular() {
		meta.Skipped = append(meta.Skipped, ".env")
	} else if envData, err := os.ReadFile(envPath); err == nil {
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

// envFile reports whether name is an env file that may hold credentials: any
// .env or .env.* except the templates projects commit on purpose.
func envFile(name string) bool {
	switch name {
	case ".env.example", ".env.sample", ".env.dist", ".env.template":
		return false
	}
	return name == ".env" || strings.HasPrefix(name, ".env.")
}

// stripSecrets blanks the values of secret-looking keys, of URLs that carry a
// password, and of PEM blocks (including quoted values that span lines). It
// returns the new content and the sorted list of stripped keys.
func stripSecrets(content string) (string, []string) {
	lines := strings.Split(content, "\n")
	var out, stripped []string
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(strings.TrimSuffix(lines[i], "\r"))
		key, val, found := strings.Cut(trimmed, "=")
		if !found || strings.HasPrefix(trimmed, "#") || val == "" {
			out = append(out, lines[i])
			continue
		}
		// A quoted value left open on this line continues to its closing quote.
		end := i
		if q := rune(val[0]); (q == '"' || q == '\'') && !strings.ContainsRune(val[1:], q) {
			for end+1 < len(lines) {
				end++
				if strings.ContainsRune(lines[end], q) {
					break
				}
			}
		}
		value := strings.Join(lines[i:end+1], "\n")
		name := strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if secretKeyRE.MatchString(name) || urlCredRE.MatchString(value) || strings.Contains(value, "-----BEGIN") {
			out = append(out, key+"=")
			stripped = append(stripped, name)
		} else {
			out = append(out, lines[i:end+1]...)
		}
		i = end
	}
	sort.Strings(stripped)
	return strings.Join(out, "\n"), stripped
}

// ReadMeta extracts manifest.json from a bundle zip without unpacking.
// Extraction limits. A bundle is untrusted input, and a zip entry can claim a
// tiny size and inflate without bound.
const (
	maxBundleEntries = 200_000
	maxBundleBytes   = 16 << 30 // all entries, uncompressed
	maxMetaBytes     = 4 << 20
)

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
		if err := json.NewDecoder(io.LimitReader(rc, maxMetaBytes)).Decode(&meta); err != nil {
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
	if len(zr.File) > maxBundleEntries {
		return nil, fmt.Errorf("bundle has %d entries, more than the %d Hull accepts", len(zr.File), maxBundleEntries)
	}

	budget := int64(maxBundleBytes)
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
		n, err := extractFile(f, target, budget)
		if err != nil {
			return nil, err
		}
		budget -= n
	}
	return meta, nil
}

// extractFile writes one entry as a new file (O_EXCL: it never follows a link
// or overwrites) and refuses to inflate past budget bytes.
func extractFile(f *zip.File, target string, budget int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, budget+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > budget {
		err = fmt.Errorf("bundle expands past %d GiB; refusing to extract more", maxBundleBytes>>30)
	}
	return n, err
}

// StrippedEnvHint formats a user warning about stripped secrets.
func StrippedEnvHint(meta *Meta) string {
	if len(meta.EnvStripped) == 0 {
		return ""
	}
	return "Stripped secret values (refill after import): " + strings.Join(meta.EnvStripped, ", ")
}
