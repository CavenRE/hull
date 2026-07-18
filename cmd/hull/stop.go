package main

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop everything Hull is running (projects, services, and the daemon)",
		Long: "Stop everything Hull is running: projects, shared services, and the\n" +
			"daemon itself. This is the single command to fully quiet Hull on a\n" +
			"machine.\n\n" +
			"When a daemon is running it stops all projects and services and then\n" +
			"shuts itself down; with no daemon Hull does the same sweep in-process.\n" +
			"The sweep spans four deduplicated sources so nothing is missed: managed\n" +
			"projects under your roots, the started ledger (which catches adopted or\n" +
			"out-of-root clusters), a safety pass over containers carrying Hull's\n" +
			"ownership label (orphans), and running shared service instances. It is\n" +
			"best-effort, so one failure never blocks the rest.\n\n" +
			"After the daemon exits, Hull probes the router and DNS ports (80, 443,\n" +
			"53 by default) and reports which were released, so it can honestly say\n" +
			"the machine is clear or flag a straggler. Project files and data volumes\n" +
			"are left untouched.",
		Example: "  hull stop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			if client, viaDaemon := a.client(); viaDaemon {
				n, err := client.StopAll(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("Stopped %d project(s)/service(s).\n", n)
				if err := client.Shutdown(ctx); err != nil {
					return fmt.Errorf("stopping daemon: %w", err)
				}
				waitDaemonGone(a.Config.HullHome)
				fmt.Println("Daemon stopped.")
			} else {
				n, err := a.Engine.StopAll(ctx)
				fmt.Printf("Stopped %d project(s)/service(s).\n", n)
				if err != nil {
					return err
				}
				fmt.Println("No daemon running.")
			}

			reportPortsFreed(a)
			return nil
		},
	})
}

// waitDaemonGone polls until the daemon's discovery file disappears (it is
// removed on shutdown) so the port report below reflects the post-exit state.
func waitDaemonGone(home string) {
	for i := 0; i < 30; i++ {
		if _, err := api.ReadDaemonFile(home); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// reportPortsFreed confirms the router/DNS ports the daemon held are now free,
// so `hull stop` can honestly state the machine is clear , or flag a straggler
// (another local proxy, or a daemon that did not exit).
func reportPortsFreed(a *app) {
	host := a.Config.Router.Loopback
	if host == "" {
		host = "127.0.0.1"
	}
	type probe struct{ label, addr string }
	var probes []probe
	if a.Config.Router.Enabled {
		if p := a.Config.Router.HTTPSPort; p > 0 {
			probes = append(probes, probe{"router https", net.JoinHostPort(host, strconv.Itoa(p))})
		}
		if p := a.Config.Router.HTTPPort; p > 0 {
			probes = append(probes, probe{"router http", net.JoinHostPort(host, strconv.Itoa(p))})
		}
	}
	if a.Config.DNS.Enabled && a.Config.DNS.Port > 0 {
		probes = append(probes, probe{"dns", net.JoinHostPort(host, strconv.Itoa(a.Config.DNS.Port))})
	}
	for _, pr := range probes {
		if portOpen(pr.addr) {
			fmt.Printf("  ! %s still listening on %s\n", pr.label, pr.addr)
		} else {
			fmt.Printf("  - %s released (%s)\n", pr.label, pr.addr)
		}
	}
}

func portOpen(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
