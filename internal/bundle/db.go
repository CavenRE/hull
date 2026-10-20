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

// Auth is the database login a command signs in with. An empty Password is
// the passwordless root of databases made before Hull kept logins.
type Auth struct {
	User     string
	Password string
}

// target resolves how to exec into a project's database: through the
// project compose service (dedicated) or the shared instance container. A
// MySQL password goes in the container's MYSQL_PWD, which the mysql and
// mariadb clients read.
func target(m *manifest.Manifest, key, projectDir string, a Auth) (svc *manifest.Service, prefix Cmd, err error) {
	svc, ok := m.Services[key]
	if !ok || svc == nil {
		return nil, Cmd{}, fmt.Errorf("project has no service %q", key)
	}
	eng, ok := templates.Engine(svc.Engine)
	if !ok || !eng.IsDatabase {
		return nil, Cmd{}, fmt.Errorf("service %q (%s) is not a database", key, svc.Engine)
	}
	var pwEnv []string
	if a.Password != "" && svc.Engine != "postgres" {
		pwEnv = []string{"-e", "MYSQL_PWD=" + a.Password}
	}
	if svc.Mode == manifest.ModeShared {
		container := templates.InstanceContainerName(svc.Engine, svc.Version)
		return svc, Cmd{Name: "docker", Args: append(append([]string{"exec", "-i"}, pwEnv...), container)}, nil
	}
	// The same -p/-f identity the engine pins, so a project's .env (COMPOSE_FILE,
	// COMPOSE_PROJECT_NAME) cannot aim a restore at another project's database.
	args := append(append([]string{"compose", "-p", m.Name, "-f", "compose.yaml", "exec", "-T"}, pwEnv...), key)
	return svc, Cmd{Dir: projectDir, Name: "docker", Args: args}, nil
}

func (a Auth) user(engine string) string {
	if a.User != "" {
		return a.User
	}
	return templates.RootUser(engine)
}

func mysqlCLI(engine string) string {
	if engine == "mariadb" {
		return "mariadb"
	}
	return "mysql"
}

// DumpCommand builds the command whose stdout is a plain-SQL dump of the
// project's database service.
func DumpCommand(m *manifest.Manifest, key, projectDir string, a Auth) (Cmd, error) {
	svc, prefix, err := target(m, key, projectDir, a)
	if err != nil {
		return Cmd{}, err
	}
	switch svc.Engine {
	case "postgres":
		prefix.Args = append(prefix.Args, "pg_dump", "-U", a.user(svc.Engine), "--no-owner", svc.Database)
	case "mysql", "mariadb":
		// One consistent snapshot without locking the tables, and the stored
		// routines and events, which mysqldump leaves out by default.
		prefix.Args = append(prefix.Args, mysqlCLI(svc.Engine)+"-dump", "-u", a.user(svc.Engine),
			"--single-transaction", "--routines", "--events", svc.Database)
	default:
		return Cmd{}, fmt.Errorf("no dump support for %s", svc.Engine)
	}
	return prefix, nil
}

// RestoreCommand builds the command that consumes a plain-SQL dump on stdin.
// It stops at the first failing statement, so a broken restore exits
// non-zero instead of reporting success over a half-loaded database.
func RestoreCommand(m *manifest.Manifest, key, projectDir string, a Auth) (Cmd, error) {
	svc, prefix, err := target(m, key, projectDir, a)
	if err != nil {
		return Cmd{}, err
	}
	switch svc.Engine {
	case "postgres":
		prefix.Args = append(prefix.Args, "psql", "-v", "ON_ERROR_STOP=1", "-U", a.user(svc.Engine), "-d", svc.Database)
	case "mysql", "mariadb":
		prefix.Args = append(prefix.Args, mysqlCLI(svc.Engine), "-u", a.user(svc.Engine), svc.Database)
	default:
		return Cmd{}, fmt.Errorf("no restore support for %s", svc.Engine)
	}
	return prefix, nil
}

// ReadyCommand builds the readiness probe for the project's database.
func ReadyCommand(m *manifest.Manifest, key, projectDir string, a Auth) (Cmd, error) {
	svc, prefix, err := target(m, key, projectDir, a)
	if err != nil {
		return Cmd{}, err
	}
	switch svc.Engine {
	case "postgres":
		prefix.Args = append(prefix.Args, "pg_isready", "-U", a.user(svc.Engine))
	case "mysql", "mariadb":
		// The socket, not 127.0.0.1: a shared instance's root signs in only there.
		prefix.Args = append(prefix.Args, mysqlCLI(svc.Engine), "-u", a.user(svc.Engine), "-e", "SELECT 1")
	default:
		return Cmd{}, fmt.Errorf("no readiness probe for %s", svc.Engine)
	}
	return prefix, nil
}

// MySQLCommand builds a mysql or mariadb client command that runs sql as login
// a, printing bare values (-N -B). It fails for a Postgres service.
func MySQLCommand(m *manifest.Manifest, key, projectDir string, a Auth, sql string) (Cmd, error) {
	svc, prefix, err := target(m, key, projectDir, a)
	if err != nil {
		return Cmd{}, err
	}
	if svc.Engine != "mysql" && svc.Engine != "mariadb" {
		return Cmd{}, fmt.Errorf("service %q (%s) is not MySQL or MariaDB", key, svc.Engine)
	}
	prefix.Args = append(prefix.Args, mysqlCLI(svc.Engine), "-u", a.user(svc.Engine), "-N", "-B", "-e", sql)
	return prefix, nil
}

var (
	// skipLineRE matches statements that would aim a restore at another
	// database or owner than the one Hull loads into.
	skipLineRE = regexp.MustCompile(`(?i)^\s*(CREATE\s+DATABASE|USE\s|\\connect\s|ALTER\s.*\sOWNER\s+TO\s)`)
	// definerRE matches the DEFINER of a view, trigger, routine or event, which
	// names a user of the server the dump came from. Without it, the object
	// belongs to the login that loads it.
	definerRE = regexp.MustCompile("DEFINER\\s*=\\s*(`[^`]*`|'[^']*'|\"[^\"]*\"|[^\\s@]+)@(`[^`]*`|'[^']*'|\"[^\"]*\"|[^\\s*]+)\\s*")
	// mysql8CollationRE matches the MySQL 8 collations MariaDB does not know.
	mysql8CollationRE = regexp.MustCompile(`utf8mb4_0900_[a-z_]+`)
	copyRE            = regexp.MustCompile(`(?i)^COPY\s.*\sFROM\s+stdin;\s*$`)
	dataRE            = regexp.MustCompile(`(?i)^\s*(INSERT|REPLACE)\s`)
	dollarTagRE       = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z_0-9]*)?\$`)
)

// mariadbSandbox opens dumps from mariadb-dump since 2024. The mariadb client
// reads it as a command; the mysql client stops at it.
const mariadbSandbox = "/*!999999\\- enable the sandbox mode */"

// FilterDump copies a SQL dump for loading into a database of the target
// engine. It drops the statements that would aim the dump at another database
// or owner (CREATE DATABASE, USE, \connect, OWNER TO), strips DEFINER clauses,
// changes MySQL 8 collations to utf8mb4_unicode_ci for MariaDB, and drops
// mariadb-dump's sandbox line for MySQL. Data stays as it is: the rules apply
// only to a line that starts outside a string or a function body, and never to
// INSERT, REPLACE or COPY rows.
func FilterDump(r io.Reader, w io.Writer, engine string) error {
	mysql := engine == "mysql" || engine == "mariadb"
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 256*1024*1024)
	lex := sqlLexer{backslash: mysql, dollars: engine == "postgres"}
	inCopy := false
	for scanner.Scan() {
		line := scanner.Text()
		top, start := lex.top(), lex.top() && !lex.inStmt
		switch {
		case inCopy:
			inCopy = line != `\.`
		case start && strings.HasPrefix(strings.TrimLeft(line, " \t"), `\`):
			// A psql meta-command (\connect, \restrict) is a whole line with no
			// semicolon, so it does not open a statement.
			if skipLineRE.MatchString(line) {
				continue
			}
		case start && copyRE.MatchString(line):
			inCopy = true
		case start && (skipLineRE.MatchString(line) || engine == "mysql" && strings.HasPrefix(line, mariadbSandbox)):
			lex.scan(line)
			continue
		case top && !dataRE.MatchString(line):
			if mysql {
				line = definerRE.ReplaceAllString(line, "")
			}
			if engine == "mariadb" {
				line = mysql8CollationRE.ReplaceAllString(line, "utf8mb4_unicode_ci")
			}
			lex.scan(line)
		default:
			lex.scan(line)
		}
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// sqlLexer follows strings, quoted names, comments and Postgres dollar-quoted
// bodies from line to line, so FilterDump can tell a line that starts a
// statement from one that continues a value or a function body.
type sqlLexer struct {
	backslash bool // MySQL strings (not names) escape with a backslash
	dollars   bool // Postgres $tag$ bodies
	quote     byte
	tag       string
	comment   bool
	inStmt    bool
}

func (l *sqlLexer) top() bool { return l.quote == 0 && l.tag == "" && !l.comment }

func (l *sqlLexer) scan(line string) {
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case l.tag != "":
			if strings.HasPrefix(line[i:], l.tag) {
				i += len(l.tag) - 1
				l.tag = ""
			}
		case l.comment:
			if strings.HasPrefix(line[i:], "*/") {
				i++
				l.comment = false
			}
		case l.quote != 0:
			switch {
			case l.backslash && l.quote != '`' && c == '\\':
				i++
			case c == l.quote && i+1 < len(line) && line[i+1] == l.quote:
				i++
			case c == l.quote:
				l.quote = 0
			}
		case strings.HasPrefix(line[i:], "--"):
			return
		case strings.HasPrefix(line[i:], "/*") && !strings.HasPrefix(line[i:], "/*!"):
			// MySQL runs the text of a /*! */ comment, so only a plain one is
			// skipped.
			l.comment = true
			i++
		case c == '\'' || c == '"' || c == '`':
			l.quote, l.inStmt = c, true
		case l.dollars && c == '$' && dollarTagRE.MatchString(line[i:]):
			l.tag = dollarTagRE.FindString(line[i:])
			l.inStmt = true
			i += len(l.tag) - 1
		case c == ';':
			l.inStmt = false
		case c != ' ' && c != '\t':
			l.inStmt = true
		}
	}
}

// DumpEngine reads which kind of database wrote a dump from its first bytes:
// "mysql" (MySQL and MariaDB), "postgres" for pg_dump's plain SQL, "pg_custom"
// for its custom format, or "" when the dump does not say. The header that
// dump tools write first wins over clues further in, which could be data.
func DumpEngine(head []byte) string {
	text := string(head)
	if strings.HasPrefix(text, "PGDMP") {
		return "pg_custom"
	}
	header := text
	if lines := strings.SplitN(text, "\n", 6); len(lines) > 5 {
		header = strings.Join(lines[:5], "\n")
	}
	switch {
	case strings.Contains(header, "MySQL") || strings.Contains(header, "MariaDB") || strings.Contains(header, "phpMyAdmin"):
		return "mysql"
	case strings.Contains(header, "PostgreSQL"):
		return "postgres"
	case strings.Contains(text, "/*!40") || strings.Contains(text, "ENGINE=InnoDB"):
		return "mysql"
	case strings.Contains(text, "pg_catalog."):
		return "postgres"
	}
	return ""
}

// CheckDumpEngine refuses a dump that the target engine cannot load, before
// anything is written to the database.
func CheckDumpEngine(head []byte, engine string) error {
	made := DumpEngine(head)
	switch {
	case made == "pg_custom":
		return fmt.Errorf("this is a pg_dump custom-format file; load it with pg_restore, or dump it again as plain SQL (pg_dump without -Fc)")
	case made == "postgres" && engine != "postgres":
		return fmt.Errorf("this is a Postgres dump, and the database is %s", engine)
	case made == "mysql" && engine == "postgres":
		return fmt.Errorf("this is a MySQL or MariaDB dump, and the database is Postgres")
	}
	return nil
}
