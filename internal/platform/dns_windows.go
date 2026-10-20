package platform

import (
	"fmt"
	"os/exec"
	"strings"
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
	return runElevated(nrptRegisterScript(tld, addr))
}

// EnsureLoopbackAlias is a no-op on Windows: the loopback stack answers every
// 127.0.0.0/8 address without an explicit alias.
func EnsureLoopbackAlias(addr string) error { return nil }

// UnregisterDNS removes Hull's NRPT rule.
func UnregisterDNS(tld string) error {
	return runElevated(nrptUnregisterScript(tld))
}

// DNSInstructions are the manual equivalent (elevated PowerShell).
func DNSInstructions(tld, addr string, port int) string {
	return "In an elevated (Administrator) PowerShell:\n  Add-DnsClientNrptRule -Namespace " + PSQuote("."+tld) + " -NameServers " + PSQuote(addr) + " -Comment " + PSQuote(nrptComment)
}

// runElevated runs a script through a UAC prompt and waits for it. The script
// travels as -EncodedCommand rather than a temp file another process could
// swap, and the elevated run's own exit code decides success.
func runElevated(script string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", elevatedLauncher(encodePowerShell(script)))
	// The daemon (console-less) can reach this via hosts/DNS sync; without
	// CREATE_NO_WINDOW the outer powershell pops a console. The UAC consent
	// dialog is a separate secure-desktop prompt, so it still appears.
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("elevated PowerShell failed (UAC declined?): %s", strings.TrimSpace(string(out)))
	}
	return nil
}
