package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	resolvedDropInDir = "/etc/systemd/resolved.conf.d"
	nmConfDir         = "/etc/NetworkManager/conf.d"
	nmDnsmasqDir      = "/etc/NetworkManager/dnsmasq.d"
)

// DNS backends Hull can drive on Linux.
const (
	backendResolved  = "systemd-resolved"
	backendNMDnsmasq = "networkmanager-dnsmasq"
)

// dnsBackend picks how Hull provides *.<tld> resolution on this machine:
// a systemd-resolved drop-in when resolved is the active resolver; otherwise
// NetworkManager's built-in dnsmasq (common on Arch/CachyOS); otherwise "" when
// neither is available (setup then leaves DNS alone).
func dnsBackend() string {
	if resolvedActive() {
		return backendResolved
	}
	if nmActive() {
		return backendNMDnsmasq
	}
	return ""
}

// DNSSupported reports whether Hull can manage *.<tld> DNS here. True when
// either systemd-resolved or NetworkManager (dnsmasq) is available , so on a
// stock CachyOS/Arch NetworkManager box `hull setup` now configures resolution
// itself instead of assuming an external resolver already exists.
func DNSSupported() (bool, string) {
	if dnsBackend() != "" {
		return true, ""
	}
	return false, "no supported resolver found (need systemd-resolved or NetworkManager) , Hull won't change your DNS"
}

// NeedsEmbeddedDNS reports whether Hull must run its own :53 resolver. The
// systemd-resolved backend routes ~<tld> lookups to 127.0.0.1:53 (Hull's DNS
// server), so it does. The NetworkManager+dnsmasq backend answers *.<tld>
// directly from dnsmasq, which already owns :53, so Hull must NOT bind it there.
func NeedsEmbeddedDNS() bool {
	return dnsBackend() != backendNMDnsmasq
}

// resolvedActive reports whether systemd-resolved is the active system
// resolver. The decisive signal is its stub listener (127.0.0.53) in
// /etc/resolv.conf; failing that, resolv.conf is often a systemd-managed
// symlink even in uplink mode, which we trust only when the service is
// actually running. Anything else (a plain file, or a symlink into
// NetworkManager/dnsmasq) counts as "not resolved" , the safe default.
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

func nmActive() bool {
	out, _ := exec.Command("systemctl", "is-active", "NetworkManager").Output()
	return strings.TrimSpace(string(out)) == "active"
}

// RegisterDNS configures *.<tld> resolution to addr (Hull's loopback IP) using
// this machine's backend. Root applies it directly; otherwise it returns the
// manual sudo steps.
func RegisterDNS(tld, addr string, port int) error {
	switch dnsBackend() {
	case backendResolved:
		return registerResolved(tld, addr, port)
	case backendNMDnsmasq:
		return registerNMDnsmasq(tld, addr)
	default:
		return &ManualStepsError{Instructions: DNSInstructions(tld, addr, port)}
	}
}

// sudoScript runs a privileged shell script through sudo, wiring the terminal
// so sudo can prompt for a password interactively , the same UX as the
// cert-trust step, so DNS setup isn't the odd one out that only prints
// commands. Returns an error when sudo is missing or there's no TTY to prompt
// on (e.g. a daemon/GUI context), so callers fall back to manual instructions.
func sudoScript(script string) error {
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("sudo not found")
	}
	cmd := exec.Command("sudo", "sh", "-c", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// registerResolved writes a systemd-resolved drop-in routing ~<tld> to Hull's
// resolver on addr:53. Root writes directly; otherwise it elevates via sudo
// (prompting on a TTY) and falls back to printed manual steps.
func registerResolved(tld, addr string, port int) error {
	if port != 53 {
		return fmt.Errorf("systemd-resolved drop-ins target port 53 , keep dns.port at 53")
	}
	path := filepath.Join(resolvedDropInDir, "hull-"+tld+".conf")
	if os.Geteuid() == 0 {
		if err := os.MkdirAll(resolvedDropInDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(dropInContent(tld, addr)), 0o644)
	}
	script := fmt.Sprintf("mkdir -p %s && printf '[Resolve]\\nDNS=%s\\nDomains=~%s\\n' > %s && systemctl restart systemd-resolved",
		resolvedDropInDir, addr, tld, path)
	if err := sudoScript(script); err != nil {
		return &ManualStepsError{Instructions: resolvedInstructions(tld, addr)}
	}
	return nil
}

// registerNMDnsmasq enables NetworkManager's dnsmasq plugin and adds a wildcard
// rule so *.<tld> resolves to addr directly , the mechanism a stock Arch/CachyOS
// box uses. dnsmasq answers on addr:53 itself, so Hull runs no resolver of its
// own here (see NeedsEmbeddedDNS).
func registerNMDnsmasq(tld, addr string) error {
	if os.Geteuid() == 0 {
		if err := os.MkdirAll(nmConfDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(nmDNSConfPath(), []byte("[main]\ndns=dnsmasq\n"), 0o644); err != nil {
			return err
		}
		if err := os.MkdirAll(nmDnsmasqDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(nmRulePath(tld), []byte("address=/."+tld+"/"+addr+"\n"), 0o644); err != nil {
			return err
		}
		return exec.Command("systemctl", "restart", "NetworkManager").Run()
	}
	script := fmt.Sprintf("mkdir -p %s %s && printf '[main]\\ndns=dnsmasq\\n' > %s && printf 'address=/.%s/%s\\n' > %s && systemctl restart NetworkManager",
		nmConfDir, nmDnsmasqDir, nmDNSConfPath(), tld, addr, nmRulePath(tld))
	if err := sudoScript(script); err != nil {
		return &ManualStepsError{Instructions: nmInstructions(tld, addr)}
	}
	return nil
}

// UnregisterDNS removes whatever DNS config Hull installed , both backends,
// best-effort , so no orphaned ~<tld> route or dnsmasq rule survives uninstall.
// Only files Hull owns (hull-*) are touched; a user's own dns= line is left be.
func UnregisterDNS(tld string) error {
	if os.Geteuid() != 0 {
		return &ManualStepsError{Instructions: unregisterInstructions(tld)}
	}
	resolvedGone := os.Remove(filepath.Join(resolvedDropInDir, "hull-"+tld+".conf")) == nil
	nmGone := os.Remove(nmRulePath(tld)) == nil
	if os.Remove(nmDNSConfPath()) == nil {
		nmGone = true
	}
	if resolvedGone {
		_ = exec.Command("systemctl", "restart", "systemd-resolved").Run()
	}
	if nmGone {
		_ = exec.Command("systemctl", "restart", "NetworkManager").Run()
	}
	return nil
}

func nmDNSConfPath() string        { return filepath.Join(nmConfDir, "hull-dns.conf") }
func nmRulePath(tld string) string { return filepath.Join(nmDnsmasqDir, "hull-"+tld+".conf") }

// DNSInstructions are the manual sudo equivalents for the active backend.
func DNSInstructions(tld, addr string, port int) string {
	if dnsBackend() == backendNMDnsmasq {
		return nmInstructions(tld, addr)
	}
	return resolvedInstructions(tld, addr)
}

func resolvedInstructions(tld, addr string) string {
	path := filepath.Join(resolvedDropInDir, "hull-"+tld+".conf")
	return fmt.Sprintf("sudo mkdir -p %s\nprintf '%s' | sudo tee %s\nsudo systemctl restart systemd-resolved",
		resolvedDropInDir, dropInContent(tld, addr), path)
}

func nmInstructions(tld, addr string) string {
	return fmt.Sprintf("sudo mkdir -p %s %s\nprintf '[main]\\ndns=dnsmasq\\n' | sudo tee %s\nprintf 'address=/.%s/%s\\n' | sudo tee %s\nsudo systemctl restart NetworkManager",
		nmConfDir, nmDnsmasqDir, nmDNSConfPath(), tld, addr, nmRulePath(tld))
}

func unregisterInstructions(tld string) string {
	return fmt.Sprintf("sudo rm -f %s %s %s\nsudo systemctl restart systemd-resolved 2>/dev/null; sudo systemctl restart NetworkManager 2>/dev/null",
		filepath.Join(resolvedDropInDir, "hull-"+tld+".conf"), nmRulePath(tld), nmDNSConfPath())
}

func dropInContent(tld, addr string) string {
	return "[Resolve]\nDNS=" + addr + "\nDomains=~" + tld + "\n"
}

// EnsureLoopbackAlias makes addr usable as a bind address. On Linux the whole
// 127.0.0.0/8 is already loopback, so this is a no-op.
func EnsureLoopbackAlias(addr string) error { return nil }
