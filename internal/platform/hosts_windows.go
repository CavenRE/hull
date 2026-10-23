package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

const windowsHostsPath = `C:\Windows\System32\drivers\etc\hosts`

func hostsFilePath() string { return windowsHostsPath }

// SyncHosts reconciles Hull's managed hosts block (browsers like Chrome
// bypass NRPT with their own resolvers, so hosts entries are the reliable
// layer on Windows). Reading needs no elevation; writing goes through one
// UAC prompt, and only when the block actually changed.
func SyncHosts(domains []string, ip, tld string) error {
	current, err := os.ReadFile(windowsHostsPath)
	if err != nil {
		return fmt.Errorf("reading hosts file: %w", err)
	}
	desired := MergeHostsBlock(string(current), HostsBlock(domains, ip, tld))
	if desired == string(current) {
		return nil
	}

	op, err := stageHosts(desired)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(op[1]) }()
	if err := runElevated(op...); err != nil {
		return fmt.Errorf("hosts file update needs elevation: %w", err)
	}
	return nil
}

// stageHosts writes desired to a temp file unprivileged and returns the
// elevated "hosts" operation that installs it, checked against its hash.
func stageHosts(desired string) ([]string, error) {
	tmp, err := os.CreateTemp("", "hull-hosts-*.txt")
	if err != nil {
		return nil, err
	}
	staged := tmp.Name()
	_, err = tmp.WriteString(desired)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(staged)
		return nil, err
	}
	sum := sha256.Sum256([]byte(desired))
	return []string{"hosts", staged, hex.EncodeToString(sum[:])}, nil
}

// RevertSystemChanges removes Hull's NRPT rule and its hosts block, in one
// UAC prompt.
func RevertSystemChanges(tld string) error {
	ops := []string{"nrpt-remove", tld}
	if current, err := os.ReadFile(windowsHostsPath); err == nil {
		if desired := MergeHostsBlock(string(current), ""); desired != string(current) {
			op, err := stageHosts(desired)
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(op[1]) }()
			ops = append(ops, op...)
		}
	}
	return runElevated(ops...)
}
