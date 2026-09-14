package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
)

func init() {
	var (
		direct  bool
		service string
	)

	cmd := &cobra.Command{
		Use:   "url [name]",
		Short: "Print a project's URL",
		Long: "Print the URL for a project, one per line, with no decoration, so it can\n" +
			"be piped straight into curl or a script. With no argument it uses the\n" +
			"project you are standing in.\n" +
			"\n" +
			"By default it prints the https address the router serves, which is the\n" +
			"stable one: it survives restarts and is what you give a browser.\n" +
			"\n" +
			"--direct prints the container's own address on 127.0.0.1 instead,\n" +
			"skipping Hull's router, TLS and DNS entirely. That is what you want for a\n" +
			"health check, a benchmark, or working out whether a problem is the app or\n" +
			"the routing in front of it.\n" +
			"\n" +
			"Two things to know about a --direct address. The port is assigned by\n" +
			"Docker when the container starts, so it changes on every restart: ask for\n" +
			"it each time rather than saving it anywhere. And a virtual-hosted app\n" +
			"needs the domain sent as a Host header, or it serves a different page\n" +
			"than the one you meant to test (curl -H \"Host: myapp.test\" ...).\n" +
			"\n" +
			"A project with more than one served container prints one line per\n" +
			"container; pass --service to pick just one.",
		Example: "  hull url\n" +
			"  hull url myapp\n" +
			"  hull url myapp --direct\n" +
			"  curl -sk $(hull url myapp)",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			eps := engine.Endpoints(p.Manifest, a.Config.TLD)
			if service != "" {
				eps = filterService(eps, service)
				if len(eps) == 0 {
					return fmt.Errorf("%s has no served service named %q", p.Name, service)
				}
			}
			if len(eps) == 0 {
				return fmt.Errorf("%s is not served, so it has no URL (give it one with: hull set %s --serve)", p.Name, p.Name)
			}

			if !direct {
				for _, ep := range eps {
					for _, host := range ep.Hosts {
						fmt.Printf("https://%s\n", host)
					}
				}
				return nil
			}

			// A published port only exists while the container does, so say that
			// plainly rather than letting a docker error surface.
			running, err := dockerx.RunningComposeProjects(cmd.Context())
			if err != nil {
				return fmt.Errorf("a published port only exists while the container does, and Docker is not answering: %w", err)
			}
			if !containsString(running, p.Name) {
				return fmt.Errorf("%s is not running, so it has no published port (start it with: hull up %s)", p.Name, p.Name)
			}
			printed := 0
			for _, ep := range eps {
				port, perr := a.Engine.PublishedPort(cmd.Context(), p, ep.Service, ep.Port)
				if perr != nil {
					fmt.Fprintf(os.Stderr, "hull: %s has no published port for %s\n", p.Name, ep.Service)
					continue
				}
				printed++
				fmt.Printf("http://127.0.0.1:%d\n", port)
			}
			// Exiting 0 having printed nothing reads as success to whatever is
			// consuming this, and hands it an empty URL.
			if printed == 0 {
				return fmt.Errorf("%s has no published port right now; it may still be starting", p.Name)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&direct, "direct", false, "print the container's 127.0.0.1 address instead of the routed https one")
	cmd.Flags().StringVar(&service, "service", "", "limit the output to one container (apps and clusters)")
	rootCmd.AddCommand(cmd)
}

// filterService narrows a project's endpoints to one container.
func filterService(eps []engine.Endpoint, service string) []engine.Endpoint {
	var out []engine.Endpoint
	for _, ep := range eps {
		if strings.EqualFold(ep.Service, service) {
			out = append(out, ep)
		}
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
