package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/manifest"
)

// findCluster fetches the named cluster via the daemon (when up) or in-process,
// returning a friendly error when it does not exist.
func (a *app) findCluster(ctx context.Context, name string) (*api.ClusterInfo, error) {
	var clusters []api.ClusterInfo
	var err error
	if client, ok := a.client(); ok {
		clusters, err = client.Clusters(ctx)
	} else {
		clusters, err = api.ClusterList(ctx, a.Config, dockerx.RunningComposeProjects)
	}
	if err != nil {
		return nil, err
	}
	for i := range clusters {
		if clusters[i].Name == name {
			return &clusters[i], nil
		}
	}
	return nil, fmt.Errorf("no cluster %q (see: hull cluster list)", name)
}

func init() {
	cluster := &cobra.Command{
		Use:   "cluster",
		Short: "Adopt and inspect multi-container clusters",
		Long: "A cluster is a Hull project made of many containers managed as one unit.\n" +
			"There are two kinds:\n" +
			"  - adopted (type: cluster): wraps a compose file you already own and\n" +
			"    edit; Hull never rewrites it.\n" +
			"  - managed (type: app): Hull renders and owns the compose file for you.\n\n" +
			"Once a cluster exists the normal lifecycle verbs operate on the whole unit:\n" +
			"hull up/down/restart/rebuild/reset <name>. This subgroup covers the parts\n" +
			"that are cluster-specific: adopting an existing stack (add), scaffolding a\n" +
			"new one (create), inspecting it (list, urls), configuring its addressing\n" +
			"(set, ingress), and mapping subdomains to services (route).\n\n" +
			"Most subcommands route through the daemon when one is reachable and fall\n" +
			"back to the in-process engine otherwise, so they work with no daemon running.\n" +
			"With no subcommand it lists clusters.",
		Example: "  hull cluster add ./my-stack --compose-root core\n" +
			"  hull cluster list\n" +
			"  hull cluster urls my-stack",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return listClusters(cmd) },
	}

	var (
		root     string
		files    []string
		profiles []string
		name     string
	)
	add := &cobra.Command{
		Use:   "add <dir>",
		Short: "Adopt an existing compose project as a cluster",
		Long: "Adopt an existing docker compose project as a Hull cluster (type: cluster)\n" +
			"without moving or rewriting any of your files. Hull only writes a hull.yaml\n" +
			"alongside the stack recording where the compose file lives and which routes\n" +
			"it serves; your compose files are never modified.\n\n" +
			"The directory must exist, contain a compose file (compose.yaml, compose.yml,\n" +
			"docker-compose.*, or one under --compose-root), and not already have a hull.yaml.\n" +
			"Use --compose-root when the compose file lives in a subfolder (e.g. core). Pass extra\n" +
			"compose files with repeatable --compose (added as -f overlays) and active\n" +
			"profiles with repeatable --profile. --name overrides the default, which is the\n" +
			"slugified folder name.\n\n" +
			"On adoption Hull seeds routes best-effort: first by parsing a Caddyfile in the\n" +
			"compose root (vhost blocks mapping subdomain to service:port), then falling\n" +
			"back to compose services that publish web-looking ports (80, 443, 3000, 4200,\n" +
			"5000, 8000, 8080, 8081, 8443, 8888, 9000). Review the result with hull cluster\n" +
			"urls and adjust with hull cluster route add/rm. Routes through the daemon when\n" +
			"reachable, else the in-process engine.",
		Args: cobra.ExactArgs(1),
		Example: "  hull cluster add ./my-stack --compose-root core\n" +
			"  hull cluster add . --profile dev\n" +
			"  hull cluster add ./legacy --name shop --compose compose.override.yml",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			req := api.AdoptClusterRequest{Dir: args[0], Name: name, ComposeRoot: root, ComposeFiles: files, Profiles: profiles}
			var created string
			if client, ok := a.client(); ok {
				created, err = client.AdoptCluster(cmd.Context(), req)
			} else {
				mf, e := a.Engine.AdoptCluster(engine.ClusterOptions{Dir: req.Dir, Name: req.Name, ComposeRoot: req.ComposeRoot, ComposeFiles: req.ComposeFiles, Profiles: req.Profiles})
				err = e
				if e == nil {
					created = mf.Name
				}
			}
			if err != nil {
				return err
			}
			fmt.Printf("✔ cluster %q adopted. Start it with: hull up %s\n", created, created)
			return nil
		},
	}
	add.Flags().StringVar(&root, "compose-root", "", "compose root within the cluster (e.g. core)")
	add.Flags().StringVar(&root, "root", "", "deprecated alias for --compose-root")
	_ = add.Flags().MarkHidden("root")
	add.Flags().StringVar(&name, "name", "", "cluster name (default: folder name)")
	add.Flags().StringArrayVar(&files, "compose", nil, "extra -f compose file (repeatable)")
	add.Flags().StringArrayVar(&profiles, "profile", nil, "active compose profile (repeatable)")
	cluster.AddCommand(add)

	cluster.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List adopted and managed clusters",
		Long: "List every cluster Hull knows about, both adopted (type: cluster) and\n" +
			"managed (type: app), reconciled with live compose state so each shows whether\n" +
			"it is currently running.\n\n" +
			"The table columns are NAME, STATE (running or stopped), ROOT (the compose-root\n" +
			"subfolder, or - when the compose file sits at the cluster root), ROUTES (count\n" +
			"of defined routes), and DIR (absolute path). Pass --json for a machine-readable\n" +
			"array of ClusterInfo objects.\n\n" +
			"Routes through the daemon when reachable, else lists in-process. This lists\n" +
			"clusters only; use hull list for regular single-container projects.",
		Args: cobra.NoArgs,
		Example: "  hull cluster list\n" +
			"  hull cluster ls\n" +
			"  hull cluster list --json",
		RunE: func(cmd *cobra.Command, args []string) error { return listClusters(cmd) },
	})

	cluster.AddCommand(&cobra.Command{
		Use:   "urls <name>",
		Short: "List every URL a cluster serves (or would serve)",
		Long: "Expand a cluster's routes into the full list of URLs it serves or would\n" +
			"serve. Unlike hull cluster route list (which shows raw route definitions),\n" +
			"this expands every subdomain plus its aliases into complete hostnames under\n" +
			"the cluster's domain suffix (its base_domain if set, otherwise Hull's TLD),\n" +
			"and filters out routes that are not served (serve=false).\n\n" +
			"The table columns are URL, SERVICE, PORT, and SERVED, with one row per\n" +
			"hostname. A footer reports the cluster's ingress mode: none means Hull only\n" +
			"lists these URLs and the cluster's own proxy actually serves them; delegate\n" +
			"and hull mean Hull routes them. Pass --json for an array of route objects.\n\n" +
			"Use this to confirm what a cluster is addressable at after adopting it or\n" +
			"editing routes. Routes through the daemon when reachable, else in-process.",
		Args: cobra.ExactArgs(1),
		Example: "  hull cluster urls tapkit\n" +
			"  hull cluster urls tapkit --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			c, err := a.findCluster(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if flagJSON {
				return printJSON(c.Routes)
			}
			if len(c.Routes) == 0 {
				fmt.Printf("%s has no routes yet. Add one with: hull cluster route add %s <subdomain> --service <svc> --port <n>\n", args[0], args[0])
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "URL\tSERVICE\tPORT\tSERVED")
			for _, rt := range c.Routes {
				served := "yes"
				if !rt.Served {
					served = "no"
				}
				for _, host := range rt.Hosts {
					_, _ = fmt.Fprintf(w, "https://%s\t%s\t%d\t%s\n", host, rt.Service, rt.Port, served)
				}
			}
			_ = w.Flush()
			switch c.Ingress {
			case "delegate", "hull":
				fmt.Printf("\ningress: %s\n", c.Ingress)
			default:
				fmt.Println("\ningress: none")
			}
			return nil
		},
	})

	var (
		createRoot        string
		createComposeRoot string
		createManaged     bool
		createContainers  []string
		createNoStart     bool
	)
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new multi-container cluster from scratch",
		Long: "Build a brand-new multi-container cluster (as opposed to hull cluster add,\n" +
			"which adopts an existing stack). Declare each container with a repeatable\n" +
			"--container flag of comma-separated key=value fields:\n\n" +
			"  name=<n>       container/service name (required)\n" +
			"  template=<t>   laravel | wordpress | plain (a Hull-scaffolded app), or\n" +
			"  image=<repo>   a raw image (mutually exclusive with template)\n" +
			"  version=<v>    image tag / framework version\n" +
			"  port=<n>       upstream/published port (required to serve a raw image)\n" +
			"  serve          route a subdomain to this container (opt-in; bare, or\n" +
			"                 serve=true|false). Omitted means not served, so only the\n" +
			"                 containers you name get a URL.\n\n" +
			"Each container needs exactly one of template= or image=. Template containers\n" +
			"carry their own default port; a served raw image must set port=. At least one\n" +
			"--container is required and the name must slug cleanly.\n\n" +
			"--managed makes Hull render and own the compose file (type: app); without it\n" +
			"a compose file you own and edit is written (type: cluster). --root picks which\n" +
			"configured root to create under (default: first), --compose-root places the\n" +
			"compose file in a subfolder, and --no-start writes the files without booting.\n\n" +
			"Routes through the daemon (streaming job output) when reachable, else runs the\n" +
			"in-process engine after a Docker check. Unless --no-start it boots the cluster.\n" +
			"After creating, review the URLs with hull cluster urls <name>.",
		Example: "  hull cluster create shop --managed \\\n" +
			"    --container name=web,template=laravel,port=8000,serve \\\n" +
			"    --container name=api,image=node:20-alpine,port=3000,serve\n" +
			"  hull cluster create demo --container name=app,template=wordpress,serve --no-start",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			containers, err := parseContainerSpecs(createContainers)
			if err != nil {
				return err
			}
			if len(containers) == 0 {
				return fmt.Errorf("at least one --container is required (e.g. --container name=web,template=laravel,serve)")
			}
			req := api.CreateClusterRequest{
				Name: args[0], Root: createRoot, ComposeRoot: createComposeRoot,
				Managed: createManaged, NoStart: createNoStart, Containers: containers,
			}
			if client, ok := a.client(); ok {
				job, err := client.CreateCluster(cmd.Context(), req)
				if err != nil {
					return err
				}
				if err := streamJob(cmd.Context(), client, job); err != nil {
					return err
				}
			} else {
				if err := dockerx.EngineCheck(cmd.Context()); err != nil {
					return err
				}
				if _, err := a.Engine.NewCluster(cmd.Context(), engine.NewClusterOptions{
					Name: req.Name, Root: req.Root, ComposeRoot: req.ComposeRoot,
					Managed: req.Managed, Containers: toEngineContainers(containers), SkipStart: createNoStart,
				}); err != nil {
					return err
				}
			}
			fmt.Printf("✔ cluster %q created. Manage it with: hull up/down %s\n", args[0], args[0])
			return nil
		},
	}
	create.Flags().StringVar(&createRoot, "root", "", "configured root to create under (default: first)")
	create.Flags().StringVar(&createComposeRoot, "compose-root", "", "subfolder for the compose file (default: .)")
	create.Flags().BoolVar(&createManaged, "managed", false, "Hull renders and owns the compose file (type: app)")
	create.Flags().StringArrayVar(&createContainers, "container", nil, "declare a container: name=..,template=..|image=..,port=..,serve (repeatable)")
	create.Flags().BoolVar(&createNoStart, "no-start", false, "create without booting containers")
	cluster.AddCommand(create)

	var (
		setBaseDomain string
		setIngress    string
	)
	setCmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set a cluster's base domain and ingress mode",
		Long: "Configure how Hull addresses a cluster's URLs. At least one of the two flags\n" +
			"is required; only the flags you pass are changed.\n\n" +
			"  --base-domain   the domain that routes nest under (e.g. tapkit.local), so a\n" +
			"                  route named api becomes api.tapkit.local. Passing an empty\n" +
			"                  string resets to Hull's TLD (<subdomain>.<tld>).\n" +
			"  --ingress       how Hull serves the URLs: none (Hull only lists them; the\n" +
			"                  cluster's own proxy serves them), delegate (Hull adds an\n" +
			"                  ingress container, see hull cluster ingress), or hull\n" +
			"                  (Hull's built-in router serves them).\n\n" +
			"The edit is surgical: only the base_domain and ingress keys in hull.yaml are\n" +
			"touched, preserving comments, blank lines, and key order. The value none is\n" +
			"stored internally as an empty string. Routes through the daemon when reachable,\n" +
			"else in-process. After changing, review the effect with hull cluster urls.",
		Example: "  hull cluster set tapkit --base-domain tapkit.local --ingress delegate\n" +
			"  hull cluster set tapkit --ingress hull\n" +
			"  hull cluster set tapkit --base-domain \"\"",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			name := args[0]
			var bd, ing *string
			if cmd.Flags().Changed("base-domain") {
				bd = &setBaseDomain
			}
			if cmd.Flags().Changed("ingress") {
				v := setIngress
				if v == "none" {
					v = ""
				}
				ing = &v
			}
			if bd == nil && ing == nil {
				return fmt.Errorf("nothing to change: pass --base-domain or --ingress")
			}
			if err := a.withDaemon(
				func(c *api.Client) error {
					return c.SetClusterConfig(cmd.Context(), name, api.SetClusterConfigRequest{BaseDomain: bd, Ingress: ing})
				},
				func() error {
					p, err := a.findProject(name)
					if err != nil {
						return err
					}
					return a.Engine.SetClusterConfig(p, engine.ClusterConfigSpec{BaseDomain: bd, Ingress: ing})
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ %s updated. See: hull cluster urls %s\n", name, name)
			return nil
		},
	}
	setCmd.Flags().StringVar(&setBaseDomain, "base-domain", "", "domain routes nest under (empty resets to the TLD)")
	setCmd.Flags().StringVar(&setIngress, "ingress", "", "how Hull serves the URLs: none | delegate | hull")
	cluster.AddCommand(setCmd)

	var (
		ingressWrite   bool
		ingressReplace string
	)
	ingressCmd := &cobra.Command{
		Use:   "ingress <name>",
		Short: "Preview or write the ingress-container overlay (delegate mode)",
		Long: "Generate the delegate-mode ingress artifacts for a cluster: a compose overlay\n" +
			"(compose.hull.yaml) that adds a caddy reverse-proxy container joined to all of\n" +
			"the cluster's networks (so it can reach internal-only services by name), plus a\n" +
			"Caddyfile (compose.hull.caddy) with one vhost per served hostname. Hull computes\n" +
			"a per-cluster loopback IP (127.0.0.x) and per-hostname network aliases.\n\n" +
			"By default this previews both files to stdout as a commented block without\n" +
			"touching anything. With --write it writes them into the compose root so you can\n" +
			"run:\n\n" +
			"  docker compose -f <base-compose> -f compose.hull.yaml up -d\n\n" +
			"Use --replace <service> when the cluster already ships its own proxy: the\n" +
			"overlay scales that service to replicas: 0 so Hull's ingress takes over. It is\n" +
			"reversible by deleting the overlay. The base compose file is never modified.\n\n" +
			"This command runs in-process only (no daemon path). Note: live serving is not\n" +
			"yet auto-wired into hull up, and the container's TLS uses its own internal CA\n" +
			"(trust integration is pending), so this mainly previews and validates the\n" +
			"generated artifacts.",
		Example: "  hull cluster ingress tapkit\n" +
			"  hull cluster ingress tapkit --write\n" +
			"  hull cluster ingress tapkit --write --replace edge_router",
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
			art, err := a.Engine.ClusterIngress(p, ingressReplace)
			if err != nil {
				return err
			}
			if ingressWrite {
				if err := a.Engine.WriteClusterIngress(p, art); err != nil {
					return err
				}
				fmt.Printf("✔ wrote compose.hull.yaml + compose.hull.caddy under %s\n", filepath.Join(p.Dir, p.Manifest.ComposeRoot))
				fmt.Printf("  ingress will bind %s:443; point %v at it.\n", art.BindIP, art.Hosts)
				return nil
			}
			fmt.Printf("# ingress binds %s:80/443 on networks %v, alias per host %v\n\n", art.BindIP, art.Networks, art.Hosts)
			fmt.Println("# ---- compose.hull.yaml ----")
			fmt.Print(string(art.Overlay))
			fmt.Println("# ---- compose.hull.caddy ----")
			fmt.Print(art.Caddyfile)
			return nil
		},
	}
	ingressCmd.Flags().BoolVar(&ingressWrite, "write", false, "write the overlay + Caddyfile into the compose root")
	ingressCmd.Flags().StringVar(&ingressReplace, "replace", "", "scale this existing proxy service to 0 so Hull's ingress takes over (reversible)")
	cluster.AddCommand(ingressCmd)

	route := &cobra.Command{
		Use:   "route",
		Short: "Assign and manage a cluster's URL routes",
		Long: "Manage the routes that map a cluster's subdomains to its compose services.\n" +
			"A route records a subdomain, the target service and port, optional alias\n" +
			"subdomains, and whether Hull serves it. Use add to create or update a route,\n" +
			"rm to remove one, and list to see the raw definitions.\n\n" +
			"Routes are what hull cluster urls expands into full hostnames. Editing routes\n" +
			"surgically updates the routes section of the cluster's hull.yaml. These verbs\n" +
			"route through the daemon when reachable, else run in-process.",
		Example: "  hull cluster route add tapkit api --service management_api --port 8081\n" +
			"  hull cluster route list tapkit\n" +
			"  hull cluster route rm tapkit api",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("usage: hull cluster route <add|rm|list> <cluster> ... (see: hull cluster route -h)")
		},
	}

	var (
		routeService string
		routePort    int
		routeAliases []string
		routeServe   bool
	)
	routeAdd := &cobra.Command{
		Use:   "add <cluster> <subdomain>",
		Short: "Assign a URL (subdomain) to one of the cluster's services",
		Long: "Assign a subdomain to one of the cluster's compose services, creating the\n" +
			"route or updating it if it already exists. The subdomain must be lowercase\n" +
			"letters, digits, and hyphens, starting with a letter.\n\n" +
			"--service and --port are both required and name the target compose service and\n" +
			"its container port. Repeat --alias to give the same service extra subdomains.\n" +
			"--serve defaults to true; pass --serve=false to record the route but leave it\n" +
			"unserved (listed only). When a compose file is present Hull best-effort checks\n" +
			"that the service exists (the check is skipped if compose is missing or cannot\n" +
			"be parsed).\n\n" +
			"The route is upserted into the cluster's hull.yaml, preserving surrounding\n" +
			"structure. Routes through the daemon when reachable, else in-process. After\n" +
			"adding, see the resulting URLs with hull cluster urls <cluster>.",
		Args: cobra.ExactArgs(2),
		Example: "  hull cluster route add tapkit api --service management_api --port 8081\n" +
			"  hull cluster route add tapkit t --service edge_router --port 8080 --alias tap\n" +
			"  hull cluster route add tapkit internal --service worker --port 9000 --serve=false",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			clusterName, sub := args[0], args[1]
			if !manifest.ValidSubdomain(sub) {
				return fmt.Errorf("invalid subdomain %q: lowercase letters, digits, and hyphens only, starting with a letter", sub)
			}
			var serve *bool
			if cmd.Flags().Changed("serve") {
				serve = &routeServe
			}
			if err := a.withDaemon(
				func(c *api.Client) error {
					return c.SetClusterRoute(cmd.Context(), clusterName, sub, api.SetRouteRequest{
						Service: routeService, Port: routePort, Aliases: routeAliases, Serve: serve,
					})
				},
				func() error {
					p, err := a.findProject(clusterName)
					if err != nil {
						return err
					}
					return a.Engine.SetClusterRoute(p, sub, engine.ClusterRouteSpec{
						Service: routeService, Port: routePort, Aliases: routeAliases, Serve: serve,
					})
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ route %s -> %s:%d assigned to %s. See: hull cluster urls %s\n", sub, routeService, routePort, clusterName, clusterName)
			return nil
		},
	}
	routeAdd.Flags().StringVar(&routeService, "service", "", "target compose service name (required)")
	routeAdd.Flags().IntVar(&routePort, "port", 0, "target container port (required)")
	routeAdd.Flags().StringArrayVar(&routeAliases, "alias", nil, "extra subdomain label for the same service (repeatable)")
	routeAdd.Flags().BoolVar(&routeServe, "serve", true, "whether Hull routes this URL (--serve=false lists it unserved)")
	_ = routeAdd.MarkFlagRequired("service")
	_ = routeAdd.MarkFlagRequired("port")
	route.AddCommand(routeAdd)

	route.AddCommand(&cobra.Command{
		Use:   "rm <cluster> <subdomain>",
		Short: "Remove a cluster route",
		Long: "Remove a route from a cluster by its subdomain. Hull validates the subdomain\n" +
			"format and checks the route exists, returning a friendly error if it does not.\n\n" +
			"The route entry is deleted from the cluster's hull.yaml, leaving the rest of\n" +
			"the manifest intact. This only removes Hull's route definition; it does not\n" +
			"touch your compose services or containers. Routes through the daemon when\n" +
			"reachable, else in-process. List current routes with hull cluster route list.",
		Args: cobra.ExactArgs(2),
		Example: "  hull cluster route rm tapkit api\n" +
			"  hull cluster route rm tapkit t",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			clusterName, sub := args[0], args[1]
			if !manifest.ValidSubdomain(sub) {
				return fmt.Errorf("invalid subdomain %q", sub)
			}
			if err := a.withDaemon(
				func(c *api.Client) error { return c.RemoveClusterRoute(cmd.Context(), clusterName, sub) },
				func() error {
					p, err := a.findProject(clusterName)
					if err != nil {
						return err
					}
					return a.Engine.RemoveClusterRoute(p, sub)
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ route %s removed from %s.\n", sub, clusterName)
			return nil
		},
	})

	route.AddCommand(&cobra.Command{
		Use:     "list <cluster>",
		Aliases: []string{"ls"},
		Short:   "List a cluster's route definitions",
		Long: "List a cluster's raw route definitions. The table columns are SUBDOMAIN,\n" +
			"SERVICE, PORT, ALIASES (comma-separated, or - when none), and SERVED.\n\n" +
			"This shows the routes as defined and does not expand aliases into full\n" +
			"hostnames; use hull cluster urls <cluster> for the complete list of URLs\n" +
			"(with the base domain applied and unserved routes filtered out). Pass --json\n" +
			"for an array of route objects. Routes through the daemon when reachable, else\n" +
			"in-process.",
		Args: cobra.ExactArgs(1),
		Example: "  hull cluster route list tapkit\n" +
			"  hull cluster route ls tapkit\n" +
			"  hull cluster route list tapkit --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			c, err := a.findCluster(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if flagJSON {
				return printJSON(c.Routes)
			}
			if len(c.Routes) == 0 {
				fmt.Printf("%s has no routes yet. Add one with: hull cluster route add %s <subdomain> --service <svc> --port <n>\n", args[0], args[0])
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "SUBDOMAIN\tSERVICE\tPORT\tALIASES\tSERVED")
			for _, rt := range c.Routes {
				served := "yes"
				if !rt.Served {
					served = "no"
				}
				aliases := "-"
				if len(rt.Aliases) > 0 {
					aliases = strings.Join(rt.Aliases, ",")
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", rt.Subdomain, rt.Service, rt.Port, aliases, served)
			}
			return w.Flush()
		},
	})

	cluster.AddCommand(route)

	rootCmd.AddCommand(cluster)
}

// listClusters prints the cluster table, shared by `hull cluster list` and a
// bare `hull cluster`.
func listClusters(cmd *cobra.Command) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	var clusters []api.ClusterInfo
	if client, ok := a.client(); ok {
		clusters, err = client.Clusters(cmd.Context())
	} else {
		clusters, err = api.ClusterList(cmd.Context(), a.Config, dockerx.RunningComposeProjects)
	}
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(clusters)
	}
	if len(clusters) == 0 {
		fmt.Println("No clusters. Adopt one with: hull cluster add <dir> --compose-root <subdir>")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSTATE\tROOT\tROUTES\tDIR")
	for _, c := range clusters {
		stateStr := "stopped"
		if c.Running {
			stateStr = "running"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", c.Name, stateStr, dash(c.ComposeRoot), len(c.Routes), c.Dir)
	}
	return w.Flush()
}

// parseContainerSpecs turns repeatable --container "name=web,template=laravel,
// port=8000,serve" flags into API container specs.
func parseContainerSpecs(specs []string) ([]api.ClusterContainerSpec, error) {
	out := make([]api.ClusterContainerSpec, 0, len(specs))
	for _, raw := range specs {
		c := api.ClusterContainerSpec{}
		for _, field := range strings.Split(raw, ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			key, val, hasVal := strings.Cut(field, "=")
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			switch key {
			case "name":
				c.Name = val
			case "template":
				c.Template = val
			case "image":
				c.Image = val
			case "version":
				c.Version = val
			case "port":
				p, err := strconv.Atoi(val)
				if err != nil {
					return nil, fmt.Errorf("container %q: port must be a number, got %q", raw, val)
				}
				c.Port = p
			case "serve":
				// bare "serve" or serve=true|false
				c.Serve = !hasVal || val == "true" || val == "1"
			default:
				return nil, fmt.Errorf("container %q: unknown field %q", raw, key)
			}
		}
		if c.Name == "" {
			return nil, fmt.Errorf("container %q: name= is required", raw)
		}
		if c.Template == "" && c.Image == "" {
			return nil, fmt.Errorf("container %q: needs template= or image=", raw)
		}
		if c.Template != "" && c.Image != "" {
			return nil, fmt.Errorf("container %q: set template= or image=, not both", raw)
		}
		// A served raw-image container needs a port to route to (template
		// containers carry their own default port).
		if c.Serve && c.Image != "" && c.Port == 0 {
			return nil, fmt.Errorf("container %q: serve needs a port= for a raw image container", raw)
		}
		out = append(out, c)
	}
	return out, nil
}

// toEngineContainers converts API container specs to the engine's shape (for
// the in-process fallback).
func toEngineContainers(specs []api.ClusterContainerSpec) []engine.ContainerSpec {
	out := make([]engine.ContainerSpec, 0, len(specs))
	for _, c := range specs {
		svcs := make([]engine.ClusterServiceSpec, 0, len(c.Services))
		for _, sv := range c.Services {
			svcs = append(svcs, engine.ClusterServiceSpec{Engine: sv.Engine, Version: sv.Version})
		}
		out = append(out, engine.ContainerSpec{
			Name: c.Name, Template: c.Template, Image: c.Image, Version: c.Version,
			Port: c.Port, Serve: c.Serve, Services: svcs,
		})
	}
	return out
}
