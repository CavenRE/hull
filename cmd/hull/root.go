package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/groups"
	"github.com/CavenRE/hull/internal/jobs"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/version"
)

var rootCmd = &cobra.Command{
	Use:   "hull",
	Short: "Composable local development environment",
	Long: "Hull provisions Docker-based local development environments and serves each\n" +
		"project at a trusted https://<name>.<tld> address, with shared databases,\n" +
		"framework scaffolding, and multi-container clusters.\n" +
		"\n" +
		"Hull is CLI-first. Every command works standalone (the headless guarantee),\n" +
		"and when a background daemon is running the same command routes\n" +
		"through the daemon's local API instead so the CLI and the desktop app share\n" +
		"one view of state. Run `hull help routing` for how that switch works.\n" +
		"\n" +
		"Common tasks:\n" +
		"  hull new <name> <template>    scaffold a project (laravel, wordpress, plain)\n" +
		"  hull up / down [name...]      start or stop projects\n" +
		"  hull list                     see every project and its URL\n" +
		"  hull services add <engine>    run a shared database/cache/mail instance\n" +
		"  hull link <project> <engine>  point a project at a shared instance\n" +
		"  hull cluster add <dir>        adopt an existing multi-container compose stack\n" +
		"  hull stop                     bring everything (and the daemon) down\n" +
		"\n" +
		"Command groups: lifecycle (up, down, restart, repair, logs, stop), projects\n" +
		"(new, rm, rebuild, reset, render, set, import, export, migrate), services and\n" +
		"linking, clusters, config and groups, daemon and diagnostics, and tooling\n" +
		"(exec, artisan, npm). Every command has detailed `--help`.\n" +
		"\n" +
		"Global flags apply everywhere: run `hull help flags`. Source-of-truth model:\n" +
		"each project is described by a small hull.yaml; the compose.yaml Hull runs is\n" +
		"generated and should never be hand-edited.",
	Version:       version.String(),
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Help topics: cross-cutting explanations surfaced via `hull help <topic>`
// (and listed under "Additional help topics" in the root help). They have no
// action; running one prints its explanation.
func init() {
	topic := func(use, short, long string) {
		rootCmd.AddCommand(&cobra.Command{
			Use:    use,
			Short:  short,
			Long:   long,
			Hidden: true, // keep the main command list focused; `hull help <topic>` still works
			RunE:   func(cmd *cobra.Command, args []string) error { fmt.Println(long); return nil },
		})
	}

	topic("routing", "How Hull chooses between the daemon and in-process execution",
		"Hull is CLI-first and hybrid. When a background daemon is running,\n"+
			"most commands route through its local HTTP API so the CLI, the daemon, and\n"+
			"the desktop app all act on one shared view of state. When no daemon is\n"+
			"running, the same commands run the identical engine code in-process, so the\n"+
			"CLI always works standalone (the headless guarantee).\n"+
			"\n"+
			"How the choice is made: the CLI reads ~/.hull/daemon.json (written by a\n"+
			"running daemon: its port, token, and PID) and probes GET /v1/status within\n"+
			"1.5 seconds. If that answers, the command uses the daemon; otherwise it runs\n"+
			"in-process. The global --no-daemon flag forces the in-process path even when\n"+
			"a daemon is up (an escape hatch when a daemon is wedged).\n"+
			"\n"+
			"Why prefer the daemon: it owns the embedded router, DNS, and the OS hosts\n"+
			"block, and reconciles routes after every change. A mutation made in-process\n"+
			"while a daemon is up would leave the daemon's view briefly stale (it self-\n"+
			"heals on its next 3-second reconcile). A few commands are always local\n"+
			"(status runs docker ps; render, export, migrate are in-process; daemon\n"+
			"status/stop talk to the daemon directly regardless of --no-daemon).\n"+
			"\n"+
			"Start a daemon with `hull daemon run` (or the desktop app); check it with\n"+
			"`hull daemon status`; stop everything including the daemon with `hull stop`.")

	topic("flags", "Global flags that apply to every command",
		"These persistent flags work on every hull command:\n"+
			"\n"+
			"  --home <dir>    Use a specific Hull home directory instead of the default\n"+
			"                  ($HULL_HOME, or ~/.hull). This sets where config.yaml and\n"+
			"                  daemon.json live, so it scopes both config and which daemon\n"+
			"                  a command talks to. Useful for a second isolated setup.\n"+
			"\n"+
			"  --no-daemon     Ignore any running daemon and run the command in-process.\n"+
			"                  Guarantees the CLI works with no daemon, and is the escape\n"+
			"                  hatch when a daemon is unresponsive. Does not affect\n"+
			"                  `hull daemon status`/`stop`, which always probe the daemon.\n"+
			"\n"+
			"  --json          Emit machine-readable JSON on read commands that support it\n"+
			"                  (list, deps, config get, config roots list, and the\n"+
			"                  services/group/cluster listers). Other commands ignore it.\n"+
			"\n"+
			"  -y, --yes       Assume yes for confirmation prompts, so destructive commands\n"+
			"                  (rm, reset, services rm, uninstall) run without prompting.\n"+
			"                  Equivalent to the -f/--force flag those commands also accept.\n"+
			"                  Off an interactive terminal, a prompt fails closed unless\n"+
			"                  --yes is given, so scripts never hang.\n"+
			"\n"+
			"Ctrl-C cancels any command cleanly, including streaming ones (logs) and long\n"+
			"jobs (rm, cluster create).")
}

// Global flags, shared by every command via the root's persistent flag set.
var (
	// flagHome overrides the Hull home directory ($HULL_HOME or ~/.hull).
	flagHome string
	// flagNoDaemon forces in-process execution even when a daemon answers.
	flagNoDaemon bool
	// flagJSON requests machine-readable output where a command supports it.
	flagJSON bool
	// flagYes assumes "yes" for confirmation prompts (scripting/CI).
	flagYes bool
)

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagHome, "home", "", "Hull home directory (default $HULL_HOME or ~/.hull)")
	pf.BoolVar(&flagNoDaemon, "no-daemon", false, "ignore any running daemon; run the command in-process")
	pf.BoolVar(&flagJSON, "json", false, "emit machine-readable JSON (list, deps, config get, services/group/cluster listers)")
	pf.BoolVarP(&flagYes, "yes", "y", false, "assume yes; do not prompt for confirmation")
}

// app bundles what every command needs.
type app struct {
	Config *config.Config
	Engine *engine.Engine
}

// loadApp loads global config (honoring --home) and constructs the engine.
// The engine is built eagerly even for commands that route to the daemon and
// never touch it, so engine.New must stay cheap and side-effect-free (no
// docker dial, no filesystem scan) , anything heavier belongs behind a call.
func loadApp() (*app, error) {
	cfg, err := config.Load(flagHome)
	if err != nil {
		return nil, err
	}
	return &app{Config: cfg, Engine: engine.New(cfg)}, nil
}

// client returns a connected daemon client, or ok=false to operate
// in-process (the headless guarantee of ADR 0002/0006). --no-daemon forces
// the in-process path even when a daemon is answering.
func (a *app) client() (*api.Client, bool) {
	if flagNoDaemon {
		return nil, false
	}
	return api.Connect(a.Config.HullHome)
}

// withDaemon runs viaDaemon when a daemon is reachable, else inProcess (the
// headless fallback). This is the single routing gate for mutating verbs, so
// "daemon-when-up" is the default instead of a per-command decision. The
// daemon path is the single writer; the in-process path runs only when no
// daemon answers (or --no-daemon is set), keeping writes single-writer.
func (a *app) withDaemon(viaDaemon func(*api.Client) error, inProcess func() error) error {
	if client, ok := a.client(); ok {
		return viaDaemon(client)
	}
	return inProcess()
}

// streamJob prints a daemon job's output live and returns its terminal error.
func streamJob(ctx context.Context, c *api.Client, job jobs.Info) error {
	final, err := c.WaitJob(ctx, job.ID, func(line string) { fmt.Println(line) })
	if err != nil {
		return err
	}
	if final.Status == jobs.StatusFailed {
		if final.Error != "" {
			return errors.New(final.Error)
		}
		return fmt.Errorf("job %s failed", final.ID)
	}
	return nil
}

// printJSON writes v as indented JSON to stdout, for the global --json flag.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// projectVolumes lists a project's named volumes, via the daemon when one is
// up (so it matches what a daemon-side reset would remove) or in-process.
func (a *app) projectVolumes(ctx context.Context, p *state.Project) ([]string, error) {
	if client, ok := a.client(); ok {
		return client.Volumes(ctx, p.Name)
	}
	return a.Engine.Volumes(ctx, p)
}

// currentProject resolves the project for the working directory, or returns
// ok=false when outside any registered project.
func (a *app) currentProject() (*state.Project, bool) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, false
	}
	return state.Current(a.Config.Roots, wd)
}

// findProject resolves a project by name with a friendly error, falling back to
// a ledger-known cluster whose directory is outside the configured roots (so an
// adopted cluster stays operable after its root was removed).
func (a *app) findProject(name string) (*state.Project, error) {
	p, err := state.Find(a.Config.Roots, name)
	if err == nil {
		return p, nil
	}
	if lp, ok := state.FindCluster(a.Config.HullHome, name); ok {
		return lp, nil
	}
	return nil, err
}

// configView returns the current config as the API shape, from the daemon
// when one is running (so it reflects live state) or from local config.yaml.
// A daemon read error is surfaced, not silently masked by stale local config
// (matches groupsView); the local path is used only when no daemon answers.
func (a *app) configView(ctx context.Context) (api.ConfigInfo, error) {
	if client, ok := a.client(); ok {
		ci, err := client.Config(ctx)
		if err != nil {
			return api.ConfigInfo{}, err
		}
		return *ci, nil
	}
	var ci api.ConfigInfo
	ci.TLD = a.Config.TLD
	ci.Roots = a.Config.Roots
	ci.Defaults.PHP = a.Config.Defaults.PHP
	ci.Defaults.Editor = a.Config.Defaults.Editor
	ci.Defaults.DBTool = a.Config.Defaults.DBTool
	return ci, nil
}

// saveConfig persists a full config view via the daemon (preferred, so the
// running daemon applies it live) or directly to config.yaml.
func (a *app) saveConfig(ctx context.Context, ci api.ConfigInfo) error {
	if client, ok := a.client(); ok {
		_, err := client.PutConfig(ctx, ci)
		return err
	}
	a.Config.TLD = ci.TLD
	a.Config.Roots = ci.Roots
	a.Config.Defaults.PHP = ci.Defaults.PHP
	a.Config.Defaults.Editor = ci.Defaults.Editor
	a.Config.Defaults.DBTool = ci.Defaults.DBTool
	return a.Config.Save()
}

// groupsView / saveGroups read and persist the virtual-group store, via the
// daemon when one is up or directly from groups.yaml.
func (a *app) groupsView(ctx context.Context) (*groups.Store, error) {
	if client, ok := a.client(); ok {
		return client.Groups(ctx)
	}
	return groups.Load(a.Config.HullHome)
}

func (a *app) saveGroups(ctx context.Context, s *groups.Store) error {
	if client, ok := a.client(); ok {
		return client.PutGroups(ctx, s)
	}
	return s.Save(a.Config.HullHome)
}

func main() {
	rootCmd.SetVersionTemplate("hull {{.Version}}\n")
	// Ctrl-C cancels the command context so a long or wedged call (e.g. a
	// streaming log tail, or a request to an unresponsive daemon) unwinds
	// cleanly instead of leaving the terminal stuck.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "hull:", err)
		os.Exit(1)
	}
}
