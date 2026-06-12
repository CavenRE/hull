package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

const resolvedDropInDir = "/etc/systemd/resolved.conf.d"

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
