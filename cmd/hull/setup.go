package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/certs"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/router"
	"github.com/CavenRE/hull/internal/templates"
)

func init() {
	var uninstall bool
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Install Hull's local root certificate into the system trust store",
		Long: "Provision Hull's local certificate authority (if it does not exist yet)\n" +
			"and install its root certificate into the OS trust store, and into\n" +
			"Firefox's own store when Firefox is present. This is what makes\n" +
			"https://<name>.<tld> load without a browser warning.\n\n" +
			"This runs locally against the system trust store; it does not route\n" +
			"through the daemon. Installing may prompt: Windows shows a confirmation\n" +
			"dialog and Linux/macOS may ask for sudo. Restart open browsers afterward\n" +
			"so they pick up the new CA.\n\n" +
			"hull setup already installs trust as one of its steps, so you usually\n" +
			"only run this directly to re-install after trust was skipped or reset,\n" +
			"or with --uninstall to remove Hull's certificate from the trust stores.",
		Example: "  hull trust\n" +
			"  hull trust --uninstall",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			dataDir := a.Config.RouterDataDir()
			if uninstall {
				if err := certs.UninstallTrust(dataDir); err != nil {
					return err
				}
				fmt.Println("✔ Hull's root certificate removed from trust stores.")
				return nil
			}
			if !certs.Trusted(dataDir) {
				fmt.Println("Provisioning local certificate authority...")
				if err := router.EnsureCA(dataDir); err != nil {
					return err
				}
			}
			if err := certs.InstallTrust(dataDir); err != nil {
				return err
			}
			fmt.Println("✔ Hull's root certificate is trusted. Browsers may need a restart.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove the certificate from trust stores")
	rootCmd.AddCommand(cmd)
}

func init() {
	var (
		skipTrust bool
		skipDNS   bool
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Enable Hull's native router and DNS on this machine",
		Long: "One-time machine setup for v2-native networking (replaces the v1 setup\n" +
			"pipeline). Run this once per machine before starting the daemon. It:\n\n" +
			"  1. enables the embedded router (ports 80/443) and DNS in config.yaml\n" +
			"  2. installs the local root certificate into the trust store\n" +
			"  3. registers *.<tld> DNS with the operating system\n" +
			"  4. provisions the shared system files\n\n" +
			"It routes through the daemon when one is running, otherwise it applies\n" +
			"the same changes in process. Steps 2 and 3 may prompt for elevation (a\n" +
			"Windows dialog, or sudo on Linux/macOS); if a step needs manual action\n" +
			"it prints the exact commands instead of failing the whole run.\n\n" +
			"Use --skip-trust when the certificate is already installed, and\n" +
			"--skip-dns when this machine already resolves *.<tld> another way (for\n" +
			"example an existing dnsmasq or NetworkManager setup) so the daemon does\n" +
			"not try to bind port 53 and collide with it. Afterwards start the daemon\n" +
			"(hull daemon run) and every running project is served at\n" +
			"https://<name>.<tld> with a trusted certificate. Verify with hull doctor.",
		Example: "  hull setup\n" +
			"  hull setup --skip-dns\n" +
			"  hull setup --skip-trust --skip-dns",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			cfg := a.Config

			// --skip-dns means this machine resolves *.tld another way (e.g. an
			// existing dnsmasq/NetworkManager setup), so don't enable the
			// embedded resolver , otherwise the daemon would try to bind :53
			// and collide with the resolver already there. If the OS doesn't
			// support Hull's DNS mechanism (e.g. systemd-resolved isn't the
			// active resolver), skip it automatically with a clear note rather
			// than enabling a resolver that can't take effect.
			if !skipDNS {
				if ok, reason := platform.DNSSupported(); !ok {
					fmt.Println("> Skipping OS DNS registration ,", reason)
					fmt.Printf("  Keep resolving *.%s the way you do now; this machine is left as-is.\n", cfg.TLD)
					fmt.Println("  (Pass --skip-dns to silence this.)")
					skipDNS = true
				}
			}

			fmt.Println("> Enabling embedded router and DNS in config")
			cfg.Router.Enabled = true
			// Hull runs its own :53 resolver only when the OS routes *.tld to it
			// (systemd-resolved, macOS resolver file, Windows NRPT). With
			// NetworkManager+dnsmasq the OS resolver answers *.tld directly, so
			// Hull must not bind :53 , DNS still gets registered below, but the
			// embedded server stays off.
			cfg.DNS.Enabled = !skipDNS && platform.NeedsEmbeddedDNS()
			if err := cfg.Save(); err != nil {
				return err
			}
			if err := templates.EnsureSystemFiles(cfg.HullHome); err != nil {
				return err
			}
			dnsState := "dns off (resolve *." + cfg.TLD + " elsewhere)"
			switch {
			case cfg.DNS.Enabled:
				dnsState = fmt.Sprintf("dns :%d (embedded)", cfg.DNS.Port)
			case !skipDNS:
				dnsState = "dns via system resolver"
			}
			fmt.Printf("  ✔ %s updated (router :%d/:%d, %s)\n",
				"config.yaml", cfg.Router.HTTPPort, cfg.Router.HTTPSPort, dnsState)

			if !skipTrust {
				fmt.Println("> Installing certificate trust (confirm the OS prompt)")
				dataDir := cfg.RouterDataDir()
				if !certs.Trusted(dataDir) {
					if err := router.EnsureCA(dataDir); err != nil {
						return err
					}
				}
				if err := certs.InstallTrust(dataDir); err != nil {
					fmt.Println("  !", err)
					fmt.Println("  Re-run later with: hull trust")
				} else {
					fmt.Println("  ✔ Root certificate trusted")
				}
			}

			if !skipDNS {
				fmt.Printf("> Registering *.%s DNS with the OS (may prompt for elevation)\n", cfg.TLD)
				if err := platform.RegisterDNS(cfg.TLD, cfg.DNS.Port); err != nil {
					var manual *platform.ManualStepsError
					if errors.As(err, &manual) {
						fmt.Println("  ! Needs elevation , run these manually:")
						fmt.Println(indent(manual.Instructions, "    "))
					} else {
						fmt.Println("  !", err)
						fmt.Println(indent(platform.DNSInstructions(cfg.TLD, cfg.DNS.Port), "    "))
					}
				} else if cfg.DNS.Enabled {
					fmt.Printf("  ✔ *.%s routes to Hull's resolver (active once the daemon is running)\n", cfg.TLD)
				} else {
					fmt.Printf("  ✔ *.%s now resolves to 127.0.0.1 via the system resolver\n", cfg.TLD)
				}
			}

			fmt.Println("> Checking ports & bind capability")
			routerPorts := []int{cfg.Router.HTTPPort, cfg.Router.HTTPSPort}
			// Grant CAP_NET_BIND_SERVICE up front for every privileged port the
			// daemon must bind , otherwise the bind silently fails with EACCES
			// and the router/DNS are dark. The embedded resolver needs :53, which
			// is privileged on every kernel (53 < ip_unprivileged_port_start),
			// so include it when DNS is enabled , the router's 80/443 may be
			// unprivileged on a tuned box yet :53 still needs the capability.
			// No-op (and no prompt) when nothing privileged is in play.
			capPorts := append([]int(nil), routerPorts...)
			if cfg.DNS.Enabled {
				capPorts = append(capPorts, cfg.DNS.Port)
			}
			capGranted := true
			if msg, err := platform.EnsurePortBind(capPorts); err != nil {
				capGranted = false
				var manual *platform.ManualStepsError
				if errors.As(err, &manual) {
					fmt.Println("  ! Privileged router ports need a capability , run:")
					fmt.Println(indent(manual.Instructions, "    "))
				} else {
					fmt.Println("  ! could not grant port-bind capability:", err)
				}
			} else if msg != "" {
				fmt.Println("  ✔", msg)
			}
			// A port held by Hull's own already-running daemon is expected (this
			// is the normal state when re-running setup), not a conflict , so
			// don't cry "in use" about ourselves.
			_, daemonUp := a.client()
			for _, port := range routerPorts {
				switch bindProbe(cfg.Router.Loopback, port) {
				case bindFree:
					fmt.Printf("  ✔ Port %d bindable\n", port)
				case bindInUse:
					if daemonUp {
						fmt.Printf("  ✔ Port %d already served by the running Hull daemon\n", port)
					} else {
						fmt.Printf("  ! Port %d is in use , stop the occupant (v1 hull-router? another web server?) or change router ports in config.yaml\n", port)
					}
				case bindDenied:
					if capGranted {
						fmt.Printf("  ✔ Port %d is privileged; the daemon binds it via CAP_NET_BIND_SERVICE\n", port)
					} else {
						fmt.Printf("  ! Port %d needs privilege , run: sudo setcap 'cap_net_bind_service=+ep' <hull binary>  (or lower net.ipv4.ip_unprivileged_port_start)\n", port)
					}
				}
			}

			fmt.Println("\nSetup complete. Start the daemon:  hull daemon run")
			fmt.Println("Then verify with:                  hull doctor")
			return nil
		},
	}
	cmd.Flags().BoolVar(&skipTrust, "skip-trust", false, "skip certificate trust installation")
	cmd.Flags().BoolVar(&skipDNS, "skip-dns", false, "skip OS DNS registration")
	rootCmd.AddCommand(cmd)
}

type bindResult int

const (
	bindFree   bindResult = iota // nothing there and we can bind it
	bindInUse                    // something is already bound (EADDRINUSE)
	bindDenied                   // privilege required to bind (EACCES/EPERM)
)

// bindProbe actually attempts to bind host:port, unlike a dial (which only
// reports whether something is already listening). This is what distinguishes a
// free port from a privileged one the daemon can't bind without
// CAP_NET_BIND_SERVICE , the exact case that silently broke the router on stock
// Linux, where the old dial-only check reported the port "free".
func bindProbe(host string, port int) bindResult {
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err == nil {
		_ = ln.Close()
		return bindFree
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return bindDenied
	}
	// EADDRINUSE and anything else: treat as occupied so we surface a warning.
	return bindInUse
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
