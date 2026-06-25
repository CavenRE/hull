package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const resolvedDropInDir = "/etc/systemd/resolved.conf.d"

// DNSSupported reports whether Hull can manage *.<tld> DNS on this machine
// using its supported mechanism. On Linux that mechanism is a systemd-resolved
// drop-in, so it only applies when systemd-resolved is the active resolver.
// Boxes that resolve via dnsmasq/NetworkManager (common on Arch/CachyOS)
// return false with a human-readable reason — `hull setup` then leaves DNS to
// the existing resolver instead of enabling an embedded one that would fight
// for :53.
func DNSSupported() (bool, string) {
	if resolvedActive() {
		return true, ""
	}
	return false, "systemd-resolved isn't this system's resolver (looks like dnsmasq/NetworkManager) — Hull won't change your DNS"
}

// resolvedActive reports whether systemd-resolved is the active system
// resolver. The decisive signal is its stub listener (127.0.0.53) in
// /etc/resolv.conf; failing that, resolv.conf is often a systemd-managed
// symlink even in uplink mode, which we trust only when the service is
// actually running. Anything else (a plain file, or a symlink into
// NetworkManager/dnsmasq) counts as "not resolved" — the safe default, since
// it just makes setup leave DNS alone.
func resolvedActive() bool {
	if b, err := os.ReadFile("/etc/resolv.conf"); err == nil &&
		strings.Contains(string(b), "127.0.0.53") {
		return true
	}
	if dest, err := os.Readlink("/etc/resolv.conf"); err == nil &&
		strings.Contains(dest, "systemd") {
		out, _ := exec.Command("systemctl", "is-active", "systemd-resolved").Output()
		return strings.TrimSpace(string(out)) == "active"
	}
	return false
}

// RegisterDNS writes a systemd-resolved drop-in routing ~<tld> to Hull's
// resolver. Works directly when running as root; otherwise returns the
// manual steps (sudo).
func RegisterDNS(tld string, port int) error {
	if port != 53 {
		return fmt.Errorf("systemd-resolved drop-ins target port 53 — keep dns.port at 53")
	}
	content := dropInContent(tld)
	path := filepath.Join(resolvedDropInDir, "hull-"+tld+".conf")
	if os.Geteuid() == 0 {
		if err := os.MkdirAll(resolvedDropInDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
		return nil // caller restarts systemd-resolved (or instructs)
	}
	return &ManualStepsError{Instructions: DNSInstructions(tld, port)}
}

// UnregisterDNS removes the drop-in (root) or returns manual steps.
func UnregisterDNS(tld string) error {
	path := filepath.Join(resolvedDropInDir, "hull-"+tld+".conf")
	if os.Geteuid() == 0 {
		_ = os.Remove(path)
		return nil
	}
	return &ManualStepsError{Instructions: "sudo rm -f " + path + " && sudo systemctl restart systemd-resolved"}
}

// DNSInstructions are the manual sudo equivalents.
func DNSInstructions(tld string, port int) string {
	path := filepath.Join(resolvedDropInDir, "hull-"+tld+".conf")
	return fmt.Sprintf(`sudo mkdir -p %s
printf '%s' | sudo tee %s
sudo systemctl restart systemd-resolved

(dnsmasq/NetworkManager setups: keep the v1 configuration — it serves the
same purpose; pick one resolver for .%s, not both.)`,
		resolvedDropInDir, dropInContent(tld), path, tld)
}

func dropInContent(tld string) string {
	return "[Resolve]\nDNS=127.0.0.1\nDomains=~" + tld + "\n"
}
