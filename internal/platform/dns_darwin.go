package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const resolverDir = "/etc/resolver"

// RegisterDNS writes the /etc/resolver/<tld> file macOS uses for per-domain
// resolvers. Root writes directly; otherwise it asks for admin rights via the
// native macOS auth dialog (osascript), so the GUI wizard isn't reduced to
// printing sudo commands. Only if that path is unavailable (headless/CI, or
// the user cancels) does it fall back to manual instructions.
func RegisterDNS(tld string, port int) error {
	path := filepath.Join(resolverDir, tld)
	if os.Geteuid() == 0 {
		if err := os.MkdirAll(resolverDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(resolverContent(port)), 0o644)
	}
	shell := fmt.Sprintf("/bin/mkdir -p %s && /usr/bin/printf '%s' > '%s'", resolverDir, resolverPrintf(port), path)
	if err := runWithAdmin(shell); err != nil {
		return &ManualStepsError{Instructions: DNSInstructions(tld, port)}
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
	if err := runWithAdmin("/bin/rm -f '" + path + "'"); err != nil {
		return &ManualStepsError{Instructions: "sudo rm -f " + path}
	}
	return nil
}

// DNSInstructions are the manual sudo equivalents. macOS resolver files
// support custom ports, so any dns.port works here.
func DNSInstructions(tld string, port int) string {
	path := filepath.Join(resolverDir, tld)
	return fmt.Sprintf("sudo mkdir -p %s\nprintf '%s' | sudo tee %s", resolverDir, resolverContent(port), path)
}

// runWithAdmin runs a /bin/sh command as root through osascript, which shows
// the standard macOS administrator-password dialog. shellCmd must not contain
// double quotes (we quote our paths with single quotes); only backslashes need
// escaping for the AppleScript string literal.
func runWithAdmin(shellCmd string) error {
	esc := strings.ReplaceAll(shellCmd, `\`, `\\`)
	script := `do shell script "` + esc + `" with administrator privileges`
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		return fmt.Errorf("admin authorization failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// resolverContent is the file body (real newlines) for direct writes.
func resolverContent(port int) string {
	if port == 53 {
		return "nameserver 127.0.0.1\n"
	}
	return fmt.Sprintf("nameserver 127.0.0.1\nport %d\n", port)
}

// resolverPrintf is the same body as a printf format string (literal \n) for
// embedding in the shell command run under osascript.
func resolverPrintf(port int) string {
	if port == 53 {
		return `nameserver 127.0.0.1\n`
	}
	return fmt.Sprintf(`nameserver 127.0.0.1\nport %d\n`, port)
}
