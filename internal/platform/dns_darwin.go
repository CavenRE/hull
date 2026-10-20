package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	resolverDir   = "/etc/resolver"
	loopbackPlist = "/Library/LaunchDaemons/dev.hull.loopback.plist"
)

// DNSSupported reports whether Hull can manage *.<tld> DNS on this machine.
// macOS always can, via a /etc/resolver/<tld> file. The reason string mirrors
// the Linux signature; it's empty when supported.
func DNSSupported() (bool, string) { return true, "" }

// NeedsEmbeddedDNS reports whether Hull runs its own :53 resolver. On macOS the
// /etc/resolver/<tld> file routes *.<tld> to 127.0.0.1:53 (Hull's server), so
// it always does.
func NeedsEmbeddedDNS() bool { return true }

// RegisterDNS writes the /etc/resolver/<tld> file macOS uses for per-domain
// resolvers. Root writes directly; otherwise it asks for admin rights via the
// native macOS auth dialog (osascript), so the GUI wizard isn't reduced to
// printing sudo commands. Only if that path is unavailable (headless/CI, or
// the user cancels) does it fall back to manual instructions.
func RegisterDNS(tld, addr string, port int) error {
	path := filepath.Join(resolverDir, tld)
	if os.Geteuid() == 0 {
		if err := os.MkdirAll(resolverDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(resolverContent(addr, port)), 0o644)
	}
	if err := adminWrite(path, resolverContent(addr, port), ""); err != nil {
		return &ManualStepsError{Instructions: DNSInstructions(tld, addr, port)}
	}
	return nil
}

// UnregisterDNS removes the resolver file (root or via admin prompt), falling
// back to a manual sudo instruction.
func UnregisterDNS(tld string) error {
	path := filepath.Join(resolverDir, tld)
	if os.Geteuid() == 0 {
		_ = os.Remove(path)
		return nil
	}
	if err := runWithAdmin("/bin/rm -f " + ShellQuote(path)); err != nil {
		return &ManualStepsError{Instructions: "sudo rm -f " + path}
	}
	return nil
}

// RevertSystemChanges removes the resolver file and the loopback-alias
// LaunchDaemon Hull installed, in one admin prompt.
func RevertSystemChanges(tld string) error {
	resolver := filepath.Join(resolverDir, tld)
	if err := runWithAdmin("/bin/rm -f " + ShellQuote(resolver) + " " + ShellQuote(loopbackPlist)); err != nil {
		return &ManualStepsError{Instructions: "sudo rm -f " + resolver + " " + loopbackPlist}
	}
	return nil
}

// DNSInstructions are the manual sudo equivalents. macOS resolver files
// support custom ports, so any dns.port works here.
func DNSInstructions(tld, addr string, port int) string {
	path := filepath.Join(resolverDir, tld)
	return fmt.Sprintf("sudo mkdir -p %s\nprintf '%s' | sudo tee %s", resolverDir, resolverContent(addr, port), path)
}

// runWithAdmin runs a /bin/sh command as root through osascript, which shows
// the standard macOS administrator-password dialog.
func runWithAdmin(shellCmd string) error {
	script := "do shell script " + appleScriptString(shellCmd) + " with administrator privileges"
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		return fmt.Errorf("admin authorization failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// resolverContent is the file body (real newlines) for direct writes.
func resolverContent(addr string, port int) string {
	if port == 53 {
		return "nameserver " + addr + "\n"
	}
	return fmt.Sprintf("nameserver %s\nport %d\n", addr, port)
}

// adminWrite stages content in a temp file and copies it to dest as root,
// after running pre (a shell command, or "") in the same admin prompt. Only
// quoted paths reach the root shell, never the content.
func adminWrite(dest, content, pre string) error {
	f, err := os.CreateTemp("", "hull-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(content))
	shell := adminInstallShell(f.Name(), hex.EncodeToString(sum[:]), dest)
	if pre != "" {
		shell = pre + " && " + shell
	}
	return runWithAdmin(shell)
}

// EnsureLoopbackAlias makes addr bindable on macOS, where only 127.0.0.1 is on
// lo0 by default , a server can't bind 127.0.0.2 without an alias. Adds the
// alias now (persisting via a LaunchDaemon so it survives reboot) through the
// admin auth dialog. No-op for 127.0.0.1, which is always present.
func EnsureLoopbackAlias(addr string) error {
	if addr == "" || addr == "127.0.0.1" {
		return nil
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>dev.hull.loopback</string>
<key>RunAtLoad</key><true/>
<key>ProgramArguments</key><array>
<string>/sbin/ifconfig</string><string>lo0</string><string>alias</string><string>%s</string><string>up</string>
</array></dict></plist>`, addr)
	// Add the alias now, and persist it for next boot.
	if err := adminWrite(loopbackPlist, body, "/sbin/ifconfig lo0 alias "+ShellQuote(addr)+" up"); err != nil {
		return &ManualStepsError{Instructions: fmt.Sprintf("sudo ifconfig lo0 alias %s up", addr)}
	}
	return nil
}
