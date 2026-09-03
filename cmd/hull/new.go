package main

import (
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/manifest"
	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/templates"
)

func init() {
	var (
		db           string
		noDB         bool
		withRedis    bool
		php          string
		fwVersion    string
		dbVersion    string
		serviceFlags []string
		serve        bool
		interactive  bool
		noStart      bool
		here         bool
	)

	cmd := &cobra.Command{
		Use:   "new <name> <template>",
		Short: "Scaffold a new project",
		Long: "Scaffold a new project from a template (laravel, wordpress, plain, or static).\n" +
			"\n" +
			"What it does: creates the project directory under your first configured\n" +
			"root, writes a hull.yaml manifest, renders compose.yaml, boots the\n" +
			"containers (unless --no-start), and routes a subdomain to it (unless\n" +
			"--serve=false), printing the final https URL.\n" +
			"\n" +
			"Smart defaults pick sensible infrastructure per template: laravel uses\n" +
			"SQLite (no DB container, like a fresh Laravel install), wordpress gets\n" +
			"MariaDB, plain and static get no database (static serves the folder with\n" +
			"nginx). Add a database with --db, layer in extra\n" +
			"infrastructure with --service (repeatable), or skip the DB with --no-db.\n" +
			"An explicit --db or --service suppresses the smart default.\n" +
			"\n" +
			"Routing: the common case runs through the daemon when one is up (it\n" +
			"streams the create job back to you). Extra --service entries and\n" +
			"interactive selection (-i) are not carried by the daemon endpoint yet, so\n" +
			"those force the in-process engine instead. Either way the result is the\n" +
			"same on disk.",
		Example: "  hull new myapp laravel\n" +
			"  hull new shop laravel --db mysql --redis\n" +
			"  hull new blog wordpress --version 6.4\n" +
			"  hull new api laravel --no-db\n" +
			"  hull new landing static\n" +
			"  hull new site laravel --here    (create ./site here, not under a root)",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			name, template := args[0], args[1]

			if interactive {
				db, withRedis, err = pickInfra()
				if err != nil {
					return err
				}
			}

			// Repeatable --service <engine[@version]>: a database engine
			// becomes the db, redis sets the redis shorthand, anything else
			// is an extra service. Explicit services suppress the smart db.
			var extra []engine.ServiceSpec
			for _, spec := range serviceFlags {
				def, version, err := services.Resolve(aliasSpec(a, spec))
				if err != nil {
					return err
				}
				switch {
				case def.IsDatabase:
					if db == "" {
						db = def.Name
						if dbVersion == "" {
							dbVersion = version
						}
					}
				case def.Name == "redis":
					withRedis = true
				default:
					extra = append(extra, engine.ServiceSpec{Engine: def.Name, Version: version})
				}
			}

			if db == "" && !noDB && !interactive {
				// Laravel defaults to SQLite (no service) like a fresh Laravel
				// install; only WordPress needs a database by default.
				if template == "wordpress" {
					db = "mariadb"
				}
			}
			if noDB {
				db = ""
			}

			var servePtr *bool
			if cmd.Flags().Changed("serve") {
				servePtr = &serve
			}

			// The daemon's create endpoint does not (yet) carry extra services
			// or the interactive picker, so those cases run in-process.
			if client, ok := a.client(); ok && !interactive && len(extra) == 0 && !here {
				job, err := client.CreateProject(cmd.Context(), api.CreateProjectRequest{
					Name: name, Template: template, DB: db, DBVersion: dbVersion,
					Redis: withRedis, PHP: php, Version: fwVersion, Serve: servePtr, SkipStart: noStart,
				})
				if err != nil {
					return err
				}
				if err := streamJob(cmd.Context(), client, job); err != nil {
					return err
				}
				fmt.Printf("✔ Project %q created.\n", name)
			} else {
				opts := engine.NewOptions{
					Name:          name,
					Template:      template,
					DB:            db,
					DBVersion:     dbVersion,
					Redis:         withRedis,
					PHP:           php,
					Version:       fwVersion,
					ExtraServices: extra,
					Serve:         servePtr,
					SkipStart:     noStart,
				}
				if here {
					wd, wderr := os.Getwd()
					if wderr != nil {
						return wderr
					}
					opts.Root = wd
				}
				dir, err := a.Engine.NewProject(cmd.Context(), opts)
				if err != nil {
					return err
				}
				// Register a project created outside any parked root so it stays
				// findable afterwards.
				if here {
					if err := a.registerProject(cmd.Context(), dir); err != nil {
						return err
					}
				}
				fmt.Printf("✔ Project created at %s\n", dir)
			}
			if !noStart {
				// The routed domain is the slugged name (e.g. "My App" -> my-app).
				domain := manifest.Slug(name)
				if domain == "" {
					domain = name
				}
				fmt.Printf("✔ %s is up at https://%s.%s\n", name, domain, a.Config.TLD)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&db, "db", "", "database engine: postgres, mysql, mariadb")
	cmd.Flags().BoolVar(&noDB, "no-db", false, "skip the default database")
	cmd.Flags().BoolVar(&withRedis, "redis", false, "add a Redis service")
	cmd.Flags().StringVar(&php, "php", "", "PHP version (e.g. 8.3)")
	cmd.Flags().StringVar(&fwVersion, "version", "", "framework version (wordpress tag or laravel constraint)")
	cmd.Flags().StringVar(&dbVersion, "db-version", "", "database engine version")
	cmd.Flags().StringArrayVar(&serviceFlags, "service", nil, "add a service: engine[@version] (repeatable), e.g. --service postgres --service redis")
	cmd.Flags().BoolVar(&serve, "serve", true, "give the project a routed domain (use --serve=false for headless)")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "pick infrastructure interactively")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "create without booting containers")
	cmd.Flags().BoolVar(&here, "here", false, "create the project in the current directory instead of your first root")
	rootCmd.AddCommand(cmd)
}

// pickInfra is the interactive infrastructure selector (v1's fzf flow).
func pickInfra() (db string, redis bool, err error) {
	choices, err := pickMany("Select infrastructure", []string{"PostgreSQL", "MySQL", "MariaDB", "Redis"})
	if err != nil {
		return "", false, err
	}
	for _, c := range choices {
		switch c {
		case "PostgreSQL":
			db = "postgres"
		case "MySQL":
			db = "mysql"
		case "MariaDB":
			db = "mariadb"
		case "Redis":
			redis = true
		}
	}
	if db != "" && !slices.Contains(templates.EngineKeys(), db) {
		return "", false, fmt.Errorf("unknown engine %q", db)
	}
	return db, redis, nil
}
