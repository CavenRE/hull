package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

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
			"(hulld, or hull daemon run) and every running project is served at\n" +
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
					fmt.Println("  (Pass --skip-dns to silence this, or switch the box to systemd-resolved.)")
					skipDNS = true
				}
			}

			fmt.Println("> Enabling embedded router and DNS in config")
			cfg.Router.Enabled = true
			cfg.DNS.Enabled = !skipDNS
			if err := cfg.Save(); err != nil {
				return err
			}
			if err := templates.EnsureSystemFiles(cfg.HullHome); err != nil {
				return err
			}
			dnsState := fmt.Sprintf("dns :%d", cfg.DNS.Port)
			if !cfg.DNS.Enabled {
				dnsState = "dns off (resolve *." + cfg.TLD + " elsewhere)"
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
				} else {
					fmt.Printf("  ✔ *.%s now resolves to 127.0.0.1 (once the daemon's DNS is running)\n", cfg.TLD)
				}
			}

			fmt.Println("> Checking router ports")
			for _, port := range []int{cfg.Router.HTTPPort, cfg.Router.HTTPSPort} {
				if portBusy(port) {
					fmt.Printf("  ! Port %d is in use , stop the occupant (v1 hull-router? IIS?) or change router ports in config.yaml\n", port)
				} else {
					fmt.Printf("  ✔ Port %d free\n", port)
				}
			}

			fmt.Println("\nSetup complete. Start the daemon:  hulld   (or: hull daemon run)")
			fmt.Println("Then verify with:                  hull doctor")
			return nil
		},
	}
	cmd.Flags().BoolVar(&skipTrust, "skip-trust", false, "skip certificate trust installation")
	cmd.Flags().BoolVar(&skipDNS, "skip-dns", false, "skip OS DNS registration")
	rootCmd.AddCommand(cmd)
}

func portBusy(port int) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
