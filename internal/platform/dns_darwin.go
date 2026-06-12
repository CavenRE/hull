package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

const resolverDir = "/etc/resolver"

// RegisterDNS writes the /etc/resolver/<tld> file macOS uses for
// per-domain resolvers. Root writes directly; otherwise manual steps.
func RegisterDNS(tld string, port int) error {
	content := resolverContent(port)
	path := filepath.Join(resolverDir, tld)
	if os.Geteuid() == 0 {
		if err := os.MkdirAll(resolverDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(content), 0o644)
	}
	return &ManualStepsError{Instructions: DNSInstructions(tld, port)}
}

// UnregisterDNS removes the resolver file (root) or returns manual steps.
func UnregisterDNS(tld string) error {
	path := filepath.Join(resolverDir, tld)
	if os.Geteuid() == 0 {
		_ = os.Remove(path)
		return nil
	}
	return &ManualStepsError{Instructions: "sudo rm -f " + path}
}

// DNSInstructions are the manual sudo equivalents. macOS resolver files
// support custom ports, so any dns.port works here.
func DNSInstructions(tld string, port int) string {
	path := filepath.Join(resolverDir, tld)
	return fmt.Sprintf("sudo mkdir -p %s\nprintf '%s' | sudo tee %s", resolverDir, resolverContent(port), path)
}

func resolverContent(port int) string {
	if port == 53 {
		return "nameserver 127.0.0.1\n"
	}
	return fmt.Sprintf("nameserver 127.0.0.1\nport %d\n", port)
}
