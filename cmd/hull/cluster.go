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
		Long: `A cluster is a Hull project that wraps an existing docker compose
project (many containers managed as one unit). Lifecycle uses the normal verbs:
hull up/down/restart/rebuild/reset <name> all operate on the whole cluster.`,
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
		Args:  cobra.ExactArgs(1),
		Example: `  hull cluster add ./my-stack --root core
  hull cluster add . --profile dev`,
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
	add.Flags().StringVar(&root, "root", "", "compose root within the cluster (e.g. core)")
	add.Flags().StringVar(&name, "name", "", "cluster name (default: folder name)")
	add.Flags().StringArrayVar(&files, "compose", nil, "extra -f compose file (repeatable)")
	add.Flags().StringArrayVar(&profiles, "profile", nil, "active compose profile (repeatable)")
	cluster.AddCommand(add)

	cluster.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List adopted and managed clusters",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
				fmt.Println("No clusters. Adopt one with: hull cluster add <dir> --root <subdir>")
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
		},
	})

	cluster.AddCommand(&cobra.Command{
		Use:   "urls <name>",
		Short: "List every URL a cluster serves (or would serve)",
		Args:  cobra.ExactArgs(1),
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
				fmt.Println("\ningress: none , Hull lists these; the cluster's own proxy serves them until ingress is enabled")
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
		Long: `Build a new multi-container project. Each --container declares one
container as comma-separated key=value fields:

  name=<n>       container/service name (required)
  template=<t>   laravel | wordpress | plain (a Hull-scaffolded app), or
  image=<repo>   a raw image (mutually exclusive with template)
  version=<v>    image tag / framework version
  port=<n>       upstream/published port (required to serve a raw image)
  serve          route a subdomain to this container (opt-in; bare, or
                 serve=true|false). Omitted means not served, so only the
                 containers you name get a URL.

--managed makes Hull render and own the compose file (type: app); otherwise a
compose file you own is written (type: cluster).`,
		Example: `  hull cluster create shop --managed \
    --container name=web,template=laravel,port=8000,serve \
    --container name=api,image=node:20-alpine,port=3000,serve`,
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
		Long: `Configure how Hull addresses a cluster's URLs.

  --base-domain   the domain routes nest under (e.g. tapkit.local). Empty
                  resets to Hull's TLD (<subdomain>.<tld>).
  --ingress       how Hull serves the URLs: none (list only; the cluster's
                  own proxy serves them), delegate, or hull.`,
		Example: `  hull cluster set tapkit --base-domain tapkit.local --ingress delegate`,
		Args:    cobra.ExactArgs(1),
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
				return fmt.Errorf("nothing to change , pass --base-domain or --ingress")
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

	var ingressWrite bool
	ingressCmd := &cobra.Command{
		Use:   "ingress <name>",
		Short: "Preview or write the ingress-container overlay (delegate mode)",
		Long: `Generate the delegate-mode ingress artifacts for a cluster: a compose
overlay adding a reverse-proxy container on the cluster's networks (so it
reaches internal-only services by name), plus its Caddy config. With --write,
they land in the compose root so you can run:

  docker compose -f <base-compose> -f compose.hull.yaml up -d

Note: live serving is not yet auto-wired into 'hull up', and the container's TLS
uses its own internal CA (trust integration is pending). This previews and
validates the generated artifacts.`,
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
			art, err := a.Engine.ClusterIngress(p)
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
	cluster.AddCommand(ingressCmd)

	route := &cobra.Command{Use: "route", Short: "Assign and manage a cluster's URL routes"}

	var (
		routeService string
		routePort    int
		routeAliases []string
		routeServe   bool
	)
	routeAdd := &cobra.Command{
		Use:   "add <cluster> <subdomain>",
		Short: "Assign a URL (subdomain) to one of the cluster's services",
		Args:  cobra.ExactArgs(2),
		Example: `  hull cluster route add tapkit api --service management_api --port 8081
  hull cluster route add tapkit t --service edge_router --port 8080 --alias tap`,
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
		Args:  cobra.ExactArgs(2),
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
		Args:    cobra.ExactArgs(1),
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
