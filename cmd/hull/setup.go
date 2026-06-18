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
		Long: `Provision Hull's local certificate authority (if needed) and install
its root certificate into the OS trust store — and Firefox's, when present.
Windows shows a confirmation dialog; Linux/macOS may ask for sudo.`,
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
		Long: `One-time machine setup for v2-native networking (replaces the v1
setup pipeline):

  1. enable the embedded router (ports 80/443) and DNS in config.yaml
  2. install the local root certificate into the trust store
  3. register *.<tld> DNS with the operating system
  4. provision shared system files

Afterwards, run the daemon (hulld, or: hull daemon run) and every running
project is served at https://<name>.<tld> with a trusted certificate.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			cfg := a.Config

			// --skip-dns means this machine resolves *.tld another way (e.g. an
			// existing dnsmasq/NetworkManager setup), so don't enable the
			// embedded resolver — otherwise the daemon would try to bind :53
			// and collide with the resolver already there.
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
						fmt.Println("  ! Needs elevation — run these manually:")
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
					fmt.Printf("  ! Port %d is in use — stop the occupant (v1 hull-router? IIS?) or change router ports in config.yaml\n", port)
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
