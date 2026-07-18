package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/services"
	"github.com/CavenRE/hull/internal/templates"
)

// listServices renders the shared instances grouped running-then-stopped, or
// the raw array with --json. Shared by bare `hull services` and `hull services
// list` so both look the same.
func listServices(cmd *cobra.Command) error {
	a, err := loadApp()
	if err != nil {
		return err
	}
	instances, err := services.NewManager(a.Config).List(cmd.Context())
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(instances)
	}
	if len(instances) == 0 {
		fmt.Println("No shared instances. Add one with: hull services add postgres@16")
		return nil
	}
	var running, stopped []services.Instance
	for _, in := range instances {
		if in.Running {
			running = append(running, in)
		} else {
			stopped = append(stopped, in)
		}
	}
	_ = serviceSection(a, "RUNNING", running)
	if len(running) > 0 && len(stopped) > 0 {
		fmt.Println()
	}
	_ = serviceSection(a, "STOPPED", stopped)
	return nil
}

// serviceSection prints one titled group of instances as an aligned table and
// reports whether it held a database (for the trust-auth footnote).
func serviceSection(a *app, title string, group []services.Instance) bool {
	if len(group) == 0 {
		return false
	}
	fmt.Printf("%s (%d)\n", title, len(group))
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tCONNECT\tWEB UI\tCONTAINER")
	hasDB := false
	for _, in := range group {
		connect := "-"
		if in.HostPort > 0 {
			connect = fmt.Sprintf("127.0.0.1:%d", in.HostPort)
			switch in.Engine {
			case "postgres":
				connect, hasDB = "postgres@"+connect, true
			case "mysql", "mariadb":
				connect, hasDB = "root@"+connect, true
			}
		}
		webUI := "-"
		if def, ok := templates.Engine(in.Engine); ok && def.UISubdomain != "" {
			webUI = fmt.Sprintf("https://%s.%s", def.UISubdomain, a.Config.TLD)
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", in.Name, connect, webUI, in.Container)
	}
	w.Flush()
	return hasDB
}
