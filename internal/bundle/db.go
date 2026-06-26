package bundle

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/templates"
)

// Cmd is a host command specification (pure data, executed by the caller
// via dockerx , keeps this package testable without an engine).
type Cmd struct {
	Dir  string
	Name string
	Args []string
}

func (c Cmd) String() string {
	return c.Name + " " + strings.Join(c.Args, " ")
}

// target resolves how to exec into a project's database: through the
// project compose service (dedicated) or the shared instance container.
func target(m *manifest.Manifest, key, projectDir string) (svc *manifest.Service, prefix Cmd, err error) {
	svc, ok := m.Services[key]
	if !ok || svc == nil {
		return nil, Cmd{}, fmt.Errorf("project has no service %q", key)
	}
	eng, ok := templates.Engine(svc.Engine)
	if !ok || !eng.IsDatabase {
		return nil, Cmd{}, fmt.Errorf("service %q (%s) is not a database", key, svc.Engine)
	}
	if svc.Mode == manifest.ModeShared {
		container := templates.InstanceContainerName(svc.Engine, svc.Version)
		return svc, Cmd{Name: "docker", Args: []string{"exec", "-i", container}}, nil
	}
	return svc, Cmd{Dir: projectDir, Name: "docker", Args: []string{"compose", "exec", "-T", key}}, nil
}

func mysqlCLI(engine string) string {
	if engine == "mariadb" {
		return "mariadb"
	}
	return "mysql"
}

// DumpCommand builds the command whose stdout is a plain-SQL dump of the
// project's database service.
func DumpCommand(m *manifest.Manifest, key, projectDir string) (Cmd, error) {
	svc, prefix, err := target(m, key, projectDir)
	if err != nil {
		return Cmd{}, err
	}
	switch svc.Engine {
	case "postgres":
		prefix.Args = append(prefix.Args, "pg_dump", "-U", "postgres", "--no-owner", svc.Database)
	case "mysql":
		prefix.Args = append(prefix.Args, "mysqldump", "-u", "root", svc.Database)
	case "mariadb":
		prefix.Args = append(prefix.Args, "mariadb-dump", "-u", "root", svc.Database)
	default:
		return Cmd{}, fmt.Errorf("no dump support for %s", svc.Engine)
	}
	return prefix, nil
}

// RestoreCommand builds the command that consumes a plain-SQL dump on stdin.
func RestoreCommand(m *manifest.Manifest, key, projectDir string) (Cmd, error) {
	svc, prefix, err := target(m, key, projectDir)
	if err != nil {
		return Cmd{}, err
	}
	switch svc.Engine {
	case "postgres":
		prefix.Args = append(prefix.Args, "psql", "-U", "postgres", "-d", svc.Database)
	case "mysql", "mariadb":
		prefix.Args = append(prefix.Args, mysqlCLI(svc.Engine), "-f", "-u", "root", svc.Database)
	default:
		return Cmd{}, fmt.Errorf("no restore support for %s", svc.Engine)
	}
	return prefix, nil
}

// ReadyCommand builds the readiness probe for the project's database.
func ReadyCommand(m *manifest.Manifest, key, projectDir string) (Cmd, error) {
	svc, prefix, err := target(m, key, projectDir)
	if err != nil {
		return Cmd{}, err
	}
	switch svc.Engine {
	case "postgres":
		prefix.Args = append(prefix.Args, "pg_isready", "-U", "postgres")
	case "mysql", "mariadb":
		prefix.Args = append(prefix.Args, mysqlCLI(svc.Engine), "-h", "127.0.0.1", "-u", "root", "-e", "SELECT 1")
	default:
		return Cmd{}, fmt.Errorf("no readiness probe for %s", svc.Engine)
	}
	return prefix, nil
}

var skipLineRE = regexp.MustCompile(`(?i)^\s*(CREATE\s+DATABASE|USE\s)`)

// FilterDump strips CREATE DATABASE / USE statements from a SQL stream so
// restores land in the already-created target database (v1 behavior).
func FilterDump(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if skipLineRE.MatchString(line) {
			continue
		}
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return scanner.Err()
}
