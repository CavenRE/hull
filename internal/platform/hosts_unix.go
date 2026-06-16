//go:build !windows

package platform

// SyncHosts is a no-op outside Windows: wildcard DNS via systemd-resolved
// (Linux) or /etc/resolver (macOS) covers every name without hosts
// entries.
func SyncHosts(domains []string) error {
	return nil
}

func hostsFilePath() string { return "/etc/hosts" }
