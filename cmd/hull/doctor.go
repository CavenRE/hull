package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/templates"
)

type checkResult struct {
	icon   string
	name   string
	detail string
	fatal  bool
}

func pass(name, detail string) checkResult { return checkResult{"✔", name, detail, false} }
func warn(name, detail string) checkResult { return checkResult{"!", name, detail, false} }
func fail(name, detail string) checkResult { return checkResult{"✖", name, detail, true} }

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the Hull environment",
		Long:  "Check the container engine, configuration, networking, and routing\npieces Hull depends on, with hints for anything broken.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			var results []checkResult

			// Container engine.
			if _, err := exec.LookPath("docker"); err != nil {
				results = append(results, fail("docker CLI", "not in PATH — install Docker or a docker-compatible engine"))
			} else {
				results = append(results, pass("docker CLI", "found"))
				if v, err := dockerx.Output(ctx, "", "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
					results = append(results, fail("container engine", "not responding — is it running?"))
				} else {
					results = append(results, pass("container engine", "server "+v))
				}
				if v, err := dockerx.Output(ctx, "", "docker", "compose", "version", "--short"); err != nil {
					results = append(results, fail("docker compose", "missing — install the compose plugin"))
				} else {
					results = append(results, pass("docker compose", v))
				}
			}

			// Config + roots.
			results = append(results, pass("config", fmt.Sprintf("tld=%s home=%s", a.Config.TLD, a.Config.HullHome)))
			for _, root := range a.Config.Roots {
				if info, err := os.Stat(root); err != nil || !info.IsDir() {
					results = append(results, warn("root "+root, "does not exist yet"))
				} else {
					results = append(results, pass("root "+root, "ok"))
				}
			}

			// System files.
			xdebug := filepath.Join(a.Config.HullHome, "system", "php", "xdebug.ini")
			if _, err := os.Stat(xdebug); err != nil {
				if err := templates.EnsureSystemFiles(a.Config.HullHome); err == nil {
					results = append(results, pass("xdebug.ini", "was missing — provisioned now"))
				} else {
					results = append(results, warn("xdebug.ini", "missing and could not be written: "+err.Error()))
				}
			} else {
				results = append(results, pass("xdebug.ini", "present"))
			}

			// Routing prerequisites (v1 router era, until Phase 4).
			if out, err := dockerx.Output(ctx, "", "docker", "network", "ls", "--format", "{{.Name}}"); err == nil {
				if containsLine(out, "caddy") {
					results = append(results, pass("caddy network", "exists"))
				} else {
					results = append(results, warn("caddy network", "missing — created automatically on first service/project start"))
				}
			}
			if out, err := dockerx.Output(ctx, "", "docker", "ps", "--format", "{{.Names}}"); err == nil {
				if containsLine(out, "hull-router") {
					results = append(results, pass("router", "hull-router running (v1 stack)"))
				} else {
					results = append(results, warn("router", "hull-router not running — https://*."+a.Config.TLD+" routing is down (v2 embedded router lands in Phase 4)"))
				}
			}

			// Wildcard DNS.
			probe := "hull-doctor-probe." + a.Config.TLD
			lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if addrs, err := net.DefaultResolver.LookupHost(lookupCtx, probe); err == nil && len(addrs) > 0 {
				results = append(results, pass("wildcard DNS", probe+" -> "+addrs[0]))
			} else {
				results = append(results, warn("wildcard DNS", probe+" does not resolve — dnsmasq/hosts setup incomplete (hosts-file setups only cover known sites)"))
			}

			// Daemon.
			if client, ok := api.Connect(a.Config.HullHome); ok {
				if st, err := client.Status(ctx); err == nil {
					results = append(results, pass("daemon", "running ("+st.Version+")"))
				}
			} else {
				results = append(results, pass("daemon", "not running (CLI operates in-process)"))
			}

			fatal := false
			for _, r := range results {
				fmt.Printf(" %s %-28s %s\n", r.icon, r.name, r.detail)
				if r.fatal {
					fatal = true
				}
			}
			if fatal {
				return fmt.Errorf("doctor found blocking problems")
			}
			fmt.Println("\nNo blocking problems found.")
			return nil
		},
	})
}

func containsLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
