package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/certs"
	"github.com/CavenRE/hull/internal/config"
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
		Args: cobra.NoArgs,
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
		rootFlag  string
		tldFlag   string
		loopFlag  string
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure and enable Hull's native router and DNS on this machine",
		Long: "One-time machine setup for v2-native networking (replaces the v1 setup\n" +
			"pipeline). Run this once per machine before starting the daemon. It:\n\n" +
			"  1. asks where your projects live, the local domain, and the loopback\n" +
			"     endpoint (127.0.0.1 to 127.0.0.8), defaulting to your current config\n" +
			"  2. enables the embedded router (ports 80/443) and DNS in config.yaml\n" +
			"  3. installs the local root certificate into the trust store\n" +
			"  4. registers *.<tld> DNS with the operating system\n" +
			"  5. provisions the shared system files\n\n" +
			"The prompts in step 1 default to your current settings, so re-running\n" +
			"setup and pressing enter changes nothing. Pass --root, --tld, and/or\n" +
			"--loopback to set them without prompting, or --yes to accept the current\n" +
			"values non-interactively. Off a terminal (installers, CI) it never\n" +
			"prompts and uses the current/flag values.\n\n" +
			"Steps 3 and 4 may prompt for elevation (a Windows dialog, or sudo on\n" +
			"Linux/macOS); if a step needs manual action it prints the exact commands\n" +
			"instead of failing the whole run.\n\n" +
			"Use --skip-trust when the certificate is already installed, and\n" +
			"--skip-dns when this machine already resolves *.<tld> another way (for\n" +
			"example an existing dnsmasq or NetworkManager setup) so the daemon does\n" +
			"not try to bind port 53 and collide with it. Afterwards start the daemon\n" +
			"(hull daemon run) and every running project is served at\n" +
			"https://<name>.<tld> with a trusted certificate. Verify with hull doctor.",
		Example: "  hull setup\n" +
			"  hull setup --tld local --loopback 3 --root ~/Sites\n" +
			"  hull setup --yes --skip-dns",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			cfg := a.Config

			// Collect the machine's core settings first: where projects live, the
			// local domain, and the loopback endpoint. Prompts default to the
			// current config (enter keeps them); --root/--tld/--loopback and --yes
			// skip the prompts, and off a terminal it uses the current values so
			// installers never hang.
			if err := collectSetupConfig(cfg, setupChoices{root: rootFlag, tld: tldFlag, loopback: loopFlag, noPrompt: flagYes}); err != nil {
				return err
			}

			// --skip-dns means this machine resolves *.tld another way (e.g. an
			// existing dnsmasq/NetworkManager setup), so don't enable the
			// embedded resolver , otherwise the daemon would try to bind :53
			// and collide with the resolver already there. If the OS doesn't
			// support Hull's DNS mechanism (e.g. systemd-resolved isn't the
			// active resolver), skip it automatically with a clear note rather
			// than enabling a resolver that can't take effect.
			if !skipDNS {
				if ok, reason := platform.DNSSupported(); !ok {
					fmt.Println("> Skipping OS DNS registration:", reason)
					fmt.Printf("  Keep resolving *.%s the way you do now; this machine is left as-is.\n", cfg.TLD)
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
				if err := platform.RegisterDNS(cfg.TLD, cfg.Router.Loopback, cfg.DNS.Port); err != nil {
					var manual *platform.ManualStepsError
					if errors.As(err, &manual) {
						fmt.Println("  ! Needs elevation. Run these manually:")
						fmt.Println(indent(manual.Instructions, "    "))
					} else {
						fmt.Println("  !", err)
						fmt.Println(indent(platform.DNSInstructions(cfg.TLD, cfg.Router.Loopback, cfg.DNS.Port), "    "))
					}
				} else if cfg.DNS.Enabled {
					fmt.Printf("  ✔ *.%s routes to Hull's resolver (active once the daemon is running)\n", cfg.TLD)
				} else {
					fmt.Printf("  ✔ *.%s now resolves to %s via the system resolver\n", cfg.TLD, cfg.Router.Loopback)
				}
			}

			fmt.Println("> Checking ports & bind capability")
			// Make Hull's loopback IP bindable. No-op on Linux/Windows (all of
			// 127/8 answers); on macOS a non-.1 address needs a lo0 alias, which
			// this adds (and persists) so the router/DNS can bind it.
			if err := platform.EnsureLoopbackAlias(cfg.Router.Loopback); err != nil {
				var manual *platform.ManualStepsError
				if errors.As(err, &manual) {
					fmt.Printf("  ! %s needs a loopback alias. Run:\n", cfg.Router.Loopback)
					fmt.Println(indent(manual.Instructions, "    "))
				} else {
					fmt.Printf("  ! could not alias %s: %v\n", cfg.Router.Loopback, err)
				}
			} else if cfg.Router.Loopback != "127.0.0.1" {
				fmt.Printf("  ✔ %s is bindable\n", cfg.Router.Loopback)
			}
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
					fmt.Println("  ! Privileged router ports need a capability. Run:")
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
						fmt.Printf("  ! Port %d is in use. Stop the occupant (v1 hull-router? another web server?) or change router ports in config.yaml\n", port)
					}
				case bindDenied:
					if capGranted {
						fmt.Printf("  ✔ Port %d is privileged; the daemon binds it via CAP_NET_BIND_SERVICE\n", port)
					} else {
						fmt.Printf("  ! Port %d needs privilege. Run: sudo setcap 'cap_net_bind_service=+ep' <hull binary>  (or lower net.ipv4.ip_unprivileged_port_start)\n", port)
					}
				}
			}

			// If a daemon is already running, it's serving the OLD config until
			// restarted , bounce it so the new router/DNS/loopback settings take
			// effect. On a fresh install the daemon isn't up yet (install.sh
			// starts it after setup), so this is a no-op there.
			if daemonUp {
				if restarted, err := platform.RestartDaemonService(); restarted && err == nil {
					fmt.Println("\n> Restarted the Hull daemon to apply the new configuration")
					fmt.Println("Setup complete. Verify with:  hull doctor")
				} else {
					fmt.Println("\nSetup complete. Restart the daemon to apply:  hull daemon stop && hull daemon run")
					fmt.Println("Then verify with:                             hull doctor")
				}
			} else {
				fmt.Println("\nSetup complete. Start the daemon:  hull daemon run")
				fmt.Println("Then verify with:                  hull doctor")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&skipTrust, "skip-trust", false, "skip certificate trust installation")
	cmd.Flags().BoolVar(&skipDNS, "skip-dns", false, "skip OS DNS registration")
	cmd.Flags().StringVar(&rootFlag, "root", "", "projects folder to park (skips the prompt)")
	cmd.Flags().StringVar(&tldFlag, "tld", "", "local domain suffix, e.g. test (skips the prompt)")
	cmd.Flags().StringVar(&loopFlag, "loopback", "", "loopback endpoint: 127.0.0.1-8 or just the octet (skips the prompt)")
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

// setupChoices carries CLI-supplied setup values; an empty string means
// "prompt for it (interactive), otherwise keep the current value".
type setupChoices struct {
	root     string
	tld      string
	loopback string
	noPrompt bool
}

var tldLabelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// collectSetupConfig fills the projects folder, local domain, and loopback
// endpoint on cfg. It prompts interactively (defaulting to the current values)
// unless a flag supplies the value, --yes is set, or there is no terminal. The
// chosen values are persisted by setup's own cfg.Save() further down.
func collectSetupConfig(cfg *config.Config, ch setupChoices) error {
	interactive := isInteractive() && !ch.noPrompt

	// Projects folder (the first parked root). Create it if it does not exist.
	defRoot := ""
	if len(cfg.Roots) > 0 {
		defRoot = cfg.Roots[0]
	} else if home, err := os.UserHomeDir(); err == nil {
		defRoot = filepath.Join(home, "Work", "Sites")
	}
	root := ch.root
	if root == "" && interactive {
		r, err := promptText("Projects folder", "Hull serves every project inside this folder.", defRoot)
		if err != nil {
			return err
		}
		root = r
	}
	if root == "" {
		root = defRoot
	}
	if root = config.ExpandPath(root); root != "" {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return fmt.Errorf("creating projects folder %s: %w", root, err)
		}
		cfg.Roots = ensureFirstRoot(cfg.Roots, root)
	}

	// Local domain (TLD).
	tld := ch.tld
	if tld == "" && interactive {
		t, err := promptText("Local domain", "Sites are served at <name>.<domain>.", cfg.TLD)
		if err != nil {
			return err
		}
		tld = t
	}
	if tld == "" {
		tld = cfg.TLD
	}
	tld = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tld)), ".")
	if tld == "" {
		tld = cfg.TLD
	}
	if !tldLabelRE.MatchString(tld) {
		return fmt.Errorf("invalid domain %q; use a single label like test or local", tld)
	}
	cfg.TLD = tld

	// Loopback endpoint (127.0.0.1 to 127.0.0.8).
	defLoop := cfg.Router.Loopback
	if defLoop == "" {
		defLoop = "127.0.0.2"
	}
	loop := ch.loopback
	if loop == "" && interactive {
		opts := make([]string, 0, 8)
		for i := 1; i <= 8; i++ {
			opts = append(opts, fmt.Sprintf("127.0.0.%d", i))
		}
		sel, err := pickOneDefault("Local endpoint  (.1 shares the standard loopback, .2 to .8 keep Hull on its own)", opts, defLoop)
		if err != nil {
			return err
		}
		loop = sel
	}
	if loop == "" {
		loop = defLoop
	}
	loop = normalizeLoopback(loop)
	if !config.ValidLoopback(loop) {
		return fmt.Errorf("invalid loopback %q; use 127.0.0.1 through 127.0.0.8", loop)
	}
	cfg.Router.Loopback = loop
	return nil
}

// ensureFirstRoot returns roots with root at the front, deduped.
func ensureFirstRoot(roots []string, root string) []string {
	out := []string{root}
	for _, r := range roots {
		if !sameRoot(r, root) {
			out = append(out, r)
		}
	}
	return out
}

// normalizeLoopback accepts a full 127.0.0.x address or just the octet (1 to 8).
func normalizeLoopback(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 1 && s[0] >= '1' && s[0] <= '8' {
		return "127.0.0." + s
	}
	return s
}
