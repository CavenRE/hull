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
		Long: "Bring down every project and shared service Hull started , including\n" +
			"adopted clusters living outside your roots , then stop the daemon so its\n" +
			"embedded router and DNS release ports 80/443/53. Project files and data\n" +
			"volumes are left untouched.",
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
