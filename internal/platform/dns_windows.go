package platform

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// DNSSupported reports whether Hull can manage *.<tld> DNS on this machine.
// Windows always can, via an NRPT rule. The reason string mirrors the Linux
// signature; it's empty when supported.
func DNSSupported() (bool, string) { return true, "" }

// NeedsEmbeddedDNS reports whether Hull runs its own :53 resolver. On Windows
// the NRPT rule routes *.<tld> to 127.0.0.1:53 (Hull's server), so it always
// does.
func NeedsEmbeddedDNS() bool { return true }

// RegisterDNS routes *.<tld> lookups to Hull's resolver at addr via an NRPT
// rule. Requires elevation , launched through a UAC prompt, like v1's hosts sync.
func RegisterDNS(tld, addr string, port int) error {
	if port != 53 {
		return fmt.Errorf("windows NRPT cannot target a custom DNS port , keep dns.port at 53")
	}
	script := fmt.Sprintf(`$ErrorActionPreference = "Stop"
Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq ".%[1]s" } | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
Add-DnsClientNrptRule -Namespace ".%[1]s" -NameServers "%[2]s" -Comment "Hull local TLD"
`, tld, addr)
	return runElevated(script)
}

// EnsureLoopbackAlias is a no-op on Windows: the loopback stack answers every
// 127.0.0.0/8 address without an explicit alias.
func EnsureLoopbackAlias(addr string) error { return nil }

// UnregisterDNS removes Hull's NRPT rule.
func UnregisterDNS(tld string) error {
	script := fmt.Sprintf(`$ErrorActionPreference = "Stop"
Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq ".%s" } | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
`, tld)
	return runElevated(script)
}

// DNSInstructions are the manual equivalent (elevated PowerShell).
func DNSInstructions(tld, addr string, port int) string {
	return fmt.Sprintf(`In an elevated (Administrator) PowerShell:
  Add-DnsClientNrptRule -Namespace ".%s" -NameServers "%s"`, tld, addr)
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
	// The daemon (console-less) can reach this via hosts/DNS sync; without
	// CREATE_NO_WINDOW the outer powershell pops a console. The UAC consent
	// dialog is a separate secure-desktop prompt, so it still appears.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("elevated PowerShell failed (UAC declined?): %s", string(out))
	}
	return nil
}
