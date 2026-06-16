package platform

import (
	"fmt"
	"os"
)

const windowsHostsPath = `C:\Windows\System32\drivers\etc\hosts`

func hostsFilePath() string { return windowsHostsPath }

// SyncHosts reconciles Hull's managed hosts block (browsers like Chrome
// bypass NRPT with their own resolvers, so hosts entries are the reliable
// layer on Windows). Reading needs no elevation; writing goes through one
// UAC prompt, and only when the block actually changed.
func SyncHosts(domains []string) error {
	current, err := os.ReadFile(windowsHostsPath)
	if err != nil {
		return fmt.Errorf("reading hosts file: %w", err)
	}
	desired := MergeHostsBlock(string(current), HostsBlock(domains))
	if desired == string(current) {
		return nil
	}

	// Stage the new content unprivileged, then copy it into place elevated.
	tmp, err := os.CreateTemp("", "hull-hosts-*.txt")
	if err != nil {
		return err
	}
	staged := tmp.Name()
	defer func() { _ = os.Remove(staged) }()
	if _, err := tmp.WriteString(desired); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	script := fmt.Sprintf(`$ErrorActionPreference = "Stop"
Copy-Item -Path '%s' -Destination '%s' -Force
`, staged, windowsHostsPath)
	if err := runElevated(script); err != nil {
		return fmt.Errorf("hosts file update needs elevation: %w", err)
	}
	return nil
}
