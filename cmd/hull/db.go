package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/bundle"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

func init() {
	db := &cobra.Command{
		Use:   "db",
		Short: "Export or import a project's database",
		Long: "Move one database of a project in or out as a SQL dump, without the rest\n" +
			"of the project (hull export and hull import carry the whole project).\n" +
			"\n" +
			"hull db export writes a dump of a project's database to a file, and hull\n" +
			"db import loads a dump file into it. Both work on the project's own\n" +
			"database container and on a database it links in a shared instance. A\n" +
			"project with more than one database needs --service to say which one.\n" +
			"Both run in-process.",
		Example: "  hull db export shop\n" +
			"  hull db import shop backup.sql",
	}
	db.AddCommand(dbExportCommand(), dbImportCommand())
	rootCmd.AddCommand(db)
}

func dbExportCommand() *cobra.Command {
	var out, service string
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Dump a project's database to a SQL file",
		Long: "Write a plain-SQL dump of a project's database to a file, for a backup or\n" +
			"to load somewhere else. A MySQL or MariaDB dump includes stored routines,\n" +
			"events and triggers, and its InnoDB tables (the default kind) are one\n" +
			"consistent snapshot.\n" +
			"\n" +
			"The file defaults to <name>.sql in the current folder, or\n" +
			"<name>-<service>.sql for a project with more than one database. An\n" +
			"--output name that ends in .gz is compressed. An existing file is\n" +
			"replaced only once the dump is complete.\n" +
			"\n" +
			"The database must be running: start the project with hull up, or a\n" +
			"shared instance with hull services start.",
		Example: "  hull db export shop\n" +
			"  hull db export shop -o backups/shop.sql.gz\n" +
			"  hull db export shop --service analytics",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			m, key, err := projectDatabase(p, service)
			if err != nil {
				return err
			}
			target := out
			if target == "" {
				target = dumpFileName(m, key)
			}
			_, admin := a.Engine.DBLogins(m, key)
			dump, err := bundle.DumpCommand(m, key, p.Dir, admin)
			if err != nil {
				return err
			}
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				return fmt.Errorf("%s is a folder; give a file name with --output", target)
			}
			fmt.Printf("Dumping %s (%s) -> %s\n", key, m.Services[key].Engine, target)
			if err := writeDump(target, func(w io.Writer) error {
				if err := dockerx.ExecCapture(cmd.Context(), dump.Dir, w, dump.Name, dump.Args...); err != nil {
					return fmt.Errorf("dumping %s: %w; the database must be running (%s)", key, err, startHint(m, key))
				}
				return nil
			}); err != nil {
				return err
			}
			if info, err := os.Stat(target); err == nil {
				fmt.Printf("✔ Wrote %s (%.1f MB)\n", target, float64(info.Size())/1024/1024)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "file to write (default <name>.sql; a .gz name is compressed)")
	cmd.Flags().StringVar(&service, "service", "", "database service to dump, for a project with more than one")
	return cmd
}

func dbImportCommand() *cobra.Command {
	var service string
	var force bool
	cmd := &cobra.Command{
		Use:   "import <name> <file>",
		Short: "Load a SQL dump into a project's database",
		Long: "Load a SQL dump (.sql, .sql.gz, or a .zip that holds a .sql file) into a\n" +
			"project's database. It asks first, because the dump replaces tables.\n" +
			"\n" +
			"Hull checks that the dump comes from the same kind of database before it\n" +
			"writes anything: a MySQL or MariaDB dump loads into either (unless it uses\n" +
			"a feature only one of them has), Postgres needs a Postgres dump, and a\n" +
			"pg_dump custom-format file is refused. The dump loads as the project's\n" +
			"own database login, so Hull drops what would aim it at another database\n" +
			"or owner: CREATE DATABASE, USE, \\connect and OWNER TO statements, and\n" +
			"DEFINER clauses. Into MariaDB it changes MySQL 8 collations to\n" +
			"utf8mb4_unicode_ci. Rows are loaded as they are. A linked database that\n" +
			"its shared instance does not have yet is created first.\n" +
			"\n" +
			"The dump loads into the existing database: a table in the dump replaces\n" +
			"the one of the same name when the dump drops it first (mysqldump does by\n" +
			"default), and other tables stay. Loading stops at the first statement\n" +
			"that fails.",
		Example: "  hull db import shop backup.sql\n" +
			"  hull db import shop ~/Downloads/live.sql.gz\n" +
			"  hull db import shop dump.sql --service analytics -f",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := a.findProject(args[0])
			if err != nil {
				return err
			}
			m, key, err := projectDatabase(p, service)
			if err != nil {
				return err
			}
			file := args[1]
			if info, err := os.Stat(file); err != nil {
				return err
			} else if !info.Mode().IsRegular() {
				return fmt.Errorf("%s is not a file", file)
			}
			s := m.Services[key]
			_, closeDump, err := openCheckedDump(file, s.Engine)
			if err != nil {
				return err
			}
			closeDump()
			if !force {
				ok, err := confirm(fmt.Sprintf("Load %s into %s's %s database %q? Tables in the dump replace the ones there now.", filepath.Base(file), m.Name, s.Engine, s.Database))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Aborted.")
					return nil
				}
			}
			if s.Mode == manifest.ModeShared {
				svcs := services.NewManager(a.Config)
				if _, err := svcs.EnsureUp(cmd.Context(), s.Engine, s.Version); err != nil {
					return err
				}
				if err := svcs.CreateDatabase(cmd.Context(), s.Engine, s.Version, s.Database); err != nil {
					return fmt.Errorf("creating database %q: %w", s.Database, err)
				}
			}
			return a.restoreDump(cmd.Context(), m, p.Dir, key, file)
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "database service to load into, for a project with more than one")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt (alias of --yes)")
	return cmd
}

// projectDatabase picks the database service a db command acts on: the one
// --service names, or the project's only database.
func projectDatabase(p *state.Project, service string) (*manifest.Manifest, string, error) {
	m := p.Manifest
	if m == nil {
		return nil, "", fmt.Errorf("%s is a legacy v1 project: adopt it first with: hull migrate %s", p.Name, p.Name)
	}
	var keys []string
	for _, key := range m.ServiceKeys() {
		if def, ok := templates.Engine(m.Services[key].Engine); ok && def.IsDatabase {
			keys = append(keys, key)
		}
	}
	switch {
	case len(keys) == 0:
		return nil, "", fmt.Errorf("%s has no database service", m.Name)
	case service != "":
		for _, key := range keys {
			if key == service {
				return m, key, nil
			}
		}
		return nil, "", fmt.Errorf("%s has no database service %q (databases: %s)", m.Name, service, strings.Join(keys, ", "))
	case len(keys) > 1:
		return nil, "", fmt.Errorf("%s has more than one database (%s); pick one with --service", m.Name, strings.Join(keys, ", "))
	}
	return m, keys[0], nil
}

// dumpFileName is the default export file: <name>.sql, or <name>-<key>.sql
// when the project has more than one database.
func dumpFileName(m *manifest.Manifest, key string) string {
	databases := 0
	for _, k := range m.ServiceKeys() {
		if def, ok := templates.Engine(m.Services[k].Engine); ok && def.IsDatabase {
			databases++
		}
	}
	if databases > 1 {
		return m.Name + "-" + key + ".sql"
	}
	return m.Name + ".sql"
}

// writeDump writes a dump beside target and renames it into place once it is
// complete, so a failed dump never leaves a truncated file where a good one
// was. A target ending in .gz is compressed.
func writeDump(target string, fill func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(target), ".hull-db-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = f.Close(); _ = os.Remove(tmp) }()
	var w io.Writer = f
	var gz *gzip.Writer
	if strings.HasSuffix(strings.ToLower(target), ".gz") {
		gz = gzip.NewWriter(f)
		w = gz
	}
	if err := fill(w); err != nil {
		return err
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
