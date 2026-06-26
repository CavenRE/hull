package platform

import (
	"fmt"
	"os"
	"os/exec"
)

// DNSSupported reports whether Hull can manage *.<tld> DNS on this machine.
// Windows always can, via an NRPT rule. The reason string mirrors the Linux
// signature; it's empty when supported.
func DNSSupported() (bool, string) { return true, "" }

// RegisterDNS routes *.<tld> lookups to Hull's resolver via an NRPT rule.
// Requires elevation , launched through a UAC prompt, like v1's hosts sync.
func RegisterDNS(tld string, port int) error {
	if port != 53 {
		return fmt.Errorf("windows NRPT cannot target a custom DNS port , keep dns.port at 53")
	}
	script := fmt.Sprintf(`$ErrorActionPreference = "Stop"
Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq ".%[1]s" } | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
Add-DnsClientNrptRule -Namespace ".%[1]s" -NameServers "127.0.0.1" -Comment "Hull local TLD"
`, tld)
	return runElevated(script)
}

// UnregisterDNS removes Hull's NRPT rule.
func UnregisterDNS(tld string) error {
	script := fmt.Sprintf(`$ErrorActionPreference = "Stop"
Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq ".%s" } | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
`, tld)
	return runElevated(script)
}

// DNSInstructions are the manual equivalent (elevated PowerShell).
func DNSInstructions(tld string, port int) string {
	return fmt.Sprintf(`In an elevated (Administrator) PowerShell:
  Add-DnsClientNrptRule -Namespace ".%s" -NameServers "127.0.0.1"`, tld)
}

// runElevated writes a temp script and runs it via a UAC prompt, waiting
// for completion.
func runElevated(script string) error {
	f, err := os.CreateTemp("", "hull-*.ps1")
	if err != nil {
		return err
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := f.WriteString(script); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`Start-Process powershell -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','%s'`, path))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("elevated PowerShell failed (UAC declined?): %s", string(out))
	}
	return nil
}
