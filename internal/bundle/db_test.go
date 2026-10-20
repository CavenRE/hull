package bundle

import (
	"strings"
	"testing"

	"github.com/CavenRE/hull/internal/manifest"
)

func projectManifest(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDumpCommandDedicated(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: postgres\n")
	cmd, err := DumpCommand(m, "db", "/proj", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "/proj" || cmd.String() != "docker compose -p shop -f compose.yaml exec -T db pg_dump -U postgres --no-owner shop" {
		t.Errorf("cmd = %+v", cmd)
	}
}

func TestDumpCommandShared(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: mariadb\n    mode: shared\n")
	cmd, err := DumpCommand(m, "db", "/proj", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "" || cmd.String() != "docker exec -i hull-mariadb-lts mariadb-dump -u root --single-transaction --routines --events shop" {
		t.Errorf("cmd = %+v", cmd)
	}
}

func TestRestoreAndReadyCommands(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: mysql\n")
	restore, err := RestoreCommand(m, "db", "/proj", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if restore.String() != "docker compose -p shop -f compose.yaml exec -T db mysql -u root shop" {
		t.Errorf("restore = %s", restore)
	}
	ready, err := ReadyCommand(m, "db", "/proj", Auth{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ready.String(), "SELECT 1") {
		t.Errorf("ready = %s", ready)
	}
}

func TestCommandsRejectNonDatabase(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  redis:\n    engine: redis\n")
	if _, err := DumpCommand(m, "redis", "/p", Auth{}); err == nil {
		t.Error("redis dump should fail")
	}
	if _, err := DumpCommand(m, "ghost", "/p", Auth{}); err == nil {
		t.Error("missing key should fail")
	}
}

func TestFilterDump(t *testing.T) {
	in := strings.NewReader(
		"CREATE DATABASE foo;\n" +
			"USE foo;\n" +
			"  use bar;\n" +
			"CREATE TABLE users (id int);\n" +
			"INSERT INTO users VALUES (1); -- USE not at start\n")
	var out strings.Builder
	if err := FilterDump(in, &out, "mysql"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "CREATE DATABASE") || strings.Contains(got, "USE foo") || strings.Contains(got, "use bar") {
		t.Errorf("filter left statements:\n%s", got)
	}
	if !strings.Contains(got, "CREATE TABLE users") || !strings.Contains(got, "INSERT INTO users") {
		t.Errorf("filter removed too much:\n%s", got)
	}
}

func TestCommandsUseTheLogin(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: mysql\n    mode: shared\n")
	restore, err := RestoreCommand(m, "db", "/proj", Auth{User: "shop", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	if got := restore.String(); got != "docker exec -i -e MYSQL_PWD=s3cret hull-mysql-8.0 mysql -u shop shop" {
		t.Errorf("restore = %s", got)
	}
	pg := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: postgres\n")
	restore, err = RestoreCommand(pg, "db", "/proj", Auth{User: "postgres", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := restore.String(); strings.Contains(got, "MYSQL_PWD") || !strings.Contains(got, "psql -v ON_ERROR_STOP=1 -U postgres -d shop") {
		t.Errorf("postgres restore = %s", got)
	}
}

func TestFilterDumpAimsTheDumpAtTheTarget(t *testing.T) {
	mysql := "/*!50013 DEFINER=`app`@`%` SQL SECURITY DEFINER */\n" +
		"/*!50017 DEFINER=`root`@`localhost`*/ /*!50003 TRIGGER t BEFORE INSERT ON users FOR EACH ROW SET NEW.a = 1 */;;\n" +
		"CREATE DEFINER=root@localhost PROCEDURE p() BEGIN END;;\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;\n" +
		"INSERT INTO posts VALUES (1,'DEFINER=`x`@`y` and utf8mb4_0900_ai_ci are text here');\n"
	var out strings.Builder
	if err := FilterDump(strings.NewReader(mysql), &out, "mariadb"); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(out.String(), "\n")
	if strings.Contains(got[0], "DEFINER=") || strings.Contains(got[1], "DEFINER=") || strings.Contains(got[2], "DEFINER") {
		t.Errorf("a DEFINER clause survived:\n%s", out.String())
	}
	if got[2] != "CREATE PROCEDURE p() BEGIN END;;" || !strings.Contains(got[0], "SQL SECURITY DEFINER */") {
		t.Errorf("stripping broke the statements:\n%s", out.String())
	}
	if !strings.Contains(got[3], "COLLATE=utf8mb4_unicode_ci") {
		t.Errorf("a MySQL 8 collation reached MariaDB: %s", got[3])
	}
	if got[4] != "INSERT INTO posts VALUES (1,'DEFINER=`x`@`y` and utf8mb4_0900_ai_ci are text here');" {
		t.Errorf("a row was rewritten: %s", got[4])
	}
	out.Reset()
	if err := FilterDump(strings.NewReader(mysql), &out, "mysql"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "utf8mb4_0900_ai_ci;") {
		t.Error("MySQL keeps its own collations")
	}

	pg := "\\connect live\n" +
		"ALTER TABLE public.users OWNER TO live_user;\n" +
		"COPY public.notes (id, body) FROM stdin;\n" +
		"1\tUSE the side door\n" +
		"2\tALTER x OWNER TO y\n" +
		"\\.\n" +
		"CREATE INDEX i ON public.notes (id);\n"
	out.Reset()
	if err := FilterDump(strings.NewReader(pg), &out, "postgres"); err != nil {
		t.Fatal(err)
	}
	want := "COPY public.notes (id, body) FROM stdin;\n1\tUSE the side door\n2\tALTER x OWNER TO y\n\\.\nCREATE INDEX i ON public.notes (id);\n"
	if out.String() != want {
		t.Errorf("postgres filter =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestCheckDumpEngine(t *testing.T) {
	for _, c := range []struct {
		head, engine string
		ok           bool
	}{
		{"-- MySQL dump 10.13  Distrib 8.0.36\n", "mariadb", true},
		{"-- MariaDB dump 10.19  Distrib 10.11.6\n", "mysql", true},
		{"-- phpMyAdmin SQL Dump\n", "mysql", true},
		{"--\n-- PostgreSQL database dump\n--\n", "postgres", true},
		{"--\n-- PostgreSQL database dump\n--\n", "mysql", false},
		{"-- MySQL dump 10.13\n", "postgres", false},
		{"PGDMP\x01\x0e", "postgres", false},
		{"CREATE TABLE t (id int);\n", "postgres", true},
	} {
		if err := CheckDumpEngine([]byte(c.head), c.engine); (err == nil) != c.ok {
			t.Errorf("CheckDumpEngine(%q, %s) = %v", c.head, c.engine, err)
		}
	}
}

func TestMySQLCommand(t *testing.T) {
	m := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: mysql\n    mode: shared\n")
	cmd, err := MySQLCommand(m, "db", "/proj", Auth{}, "SELECT 1")
	if err != nil || cmd.String() != "docker exec -i hull-mysql-8.0 mysql -u root -N -B -e SELECT 1" {
		t.Errorf("cmd = %s, err = %v", cmd, err)
	}
	pg := projectManifest(t, "schema: 1\nname: shop\ntemplate: laravel\nservices:\n  db:\n    engine: postgres\n")
	if _, err := MySQLCommand(pg, "db", "/proj", Auth{}, "SELECT 1"); err == nil {
		t.Error("a Postgres service has no mysql client")
	}
}

// Rows and function bodies span lines, so the rules must apply only to a line
// that starts a statement.
func TestFilterDumpLeavesMultiLineValuesAlone(t *testing.T) {
	pg := "INSERT INTO public.notes VALUES (1, 'first line\n" +
		"USE the side door\n" +
		"ALTER TABLE t OWNER TO u; still the same value');\n" +
		"CREATE FUNCTION public.f() RETURNS void LANGUAGE plpgsql AS $_$\n" +
		"BEGIN\n" +
		"  ALTER TABLE t OWNER TO u;\n" +
		"END;\n" +
		"$_$;\n" +
		"ALTER FUNCTION public.f() OWNER TO live;\n" +
		"ALTER SEQUENCE public.s OWNED BY public.t.id;\n"
	var out strings.Builder
	if err := FilterDump(strings.NewReader(pg), &out, "postgres"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(pg, "ALTER FUNCTION public.f() OWNER TO live;\n", "", 1)
	if out.String() != want {
		t.Errorf("postgres filter =\n%s\nwant\n%s", out.String(), want)
	}

	mysql := "/*!999999\\- enable the sandbox mode */\n" +
		"REPLACE INTO `posts` VALUES (1,'DEFINER=`x`@`y` and \\'utf8mb4_0900_ai_ci\\' are text');\n" +
		"INSERT INTO `t` VALUES ('it\\'s; USE x');\n" +
		"USE `other`;\n"
	out.Reset()
	if err := FilterDump(strings.NewReader(mysql), &out, "mysql"); err != nil {
		t.Fatal(err)
	}
	want = "REPLACE INTO `posts` VALUES (1,'DEFINER=`x`@`y` and \\'utf8mb4_0900_ai_ci\\' are text');\n" +
		"INSERT INTO `t` VALUES ('it\\'s; USE x');\n"
	if out.String() != want {
		t.Errorf("mysql filter =\n%s\nwant\n%s", out.String(), want)
	}
	out.Reset()
	if err := FilterDump(strings.NewReader(mysql), &out, "mariadb"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "/*!999999\\- enable the sandbox mode */\n") {
		t.Errorf("MariaDB reads its own sandbox line, so it stays:\n%s", out.String())
	}
}

func TestDumpHeaderWinsOverData(t *testing.T) {
	head := "-- MySQL dump 10.13  Distrib 8.0.36\n--\n-- Host: localhost\n\nINSERT INTO `docs` VALUES (1,'see pg_catalog.pg_class and the PostgreSQL database dump notes');\n"
	if got := DumpEngine([]byte(head)); got != "mysql" {
		t.Errorf("DumpEngine = %q, want mysql", got)
	}
	if err := CheckDumpEngine([]byte(head), "mariadb"); err != nil {
		t.Errorf("a MySQL dump that mentions Postgres in its data was refused: %v", err)
	}
	adminer := "-- Adminer 4.8.1 MySQL 8.0.36 dump\n\nSET NAMES utf8;\n"
	if got := DumpEngine([]byte(adminer)); got != "mysql" {
		t.Errorf("Adminer's MySQL export = %q", got)
	}
}

// pg_dump 17.6 and later wrap a plain dump in \restrict and \unrestrict, which
// psql runs; they must pass, and must not hide the statement after them.
func TestFilterDumpPassesPsqlMetaCommands(t *testing.T) {
	in := "\\restrict k3y\nALTER TABLE public.t OWNER TO live;\nCREATE TABLE public.u (id int);\n\\unrestrict k3y\n"
	var out strings.Builder
	if err := FilterDump(strings.NewReader(in), &out, "postgres"); err != nil {
		t.Fatal(err)
	}
	if want := "\\restrict k3y\nCREATE TABLE public.u (id int);\n\\unrestrict k3y\n"; out.String() != want {
		t.Errorf("filter =\n%s\nwant\n%s", out.String(), want)
	}
}
