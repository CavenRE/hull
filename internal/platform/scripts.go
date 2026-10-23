package platform

import (
	"path"
	"strings"
)

// The builders here produce the text and argv Hull runs with elevation. They
// are pure so each OS's output is testable anywhere, and they quote every
// value, so nothing a caller passes is read as code.

// ShellQuote quotes s as one POSIX shell word.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PSQuote quotes s as a PowerShell single-quoted string. PowerShell reads the
// typographic quotes U+2018 to U+201B as single quotes too, so each is doubled
// like an ASCII one.
func PSQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '‘', '’', '‚', '‛':
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// appleScriptString quotes s as an AppleScript string literal.
func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// nrptComment tags the NRPT rule Hull adds, so Hull removes only its own.
const nrptComment = "Hull local TLD"

func nrptRegisterScript(tld, addr string) string {
	ns := PSQuote("." + tld)
	return `$ErrorActionPreference = 'Stop'
Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq ` + ns + ` -and $_.Comment -eq ` + PSQuote(nrptComment) + ` } | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
Add-DnsClientNrptRule -Namespace ` + ns + ` -NameServers ` + PSQuote(addr) + ` -Comment ` + PSQuote(nrptComment) + `
`
}

func nrptUnregisterScript(tld string) string {
	return `$ErrorActionPreference = 'Stop'
Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq ` + PSQuote("."+tld) + ` -and $_.Comment -eq ` + PSQuote(nrptComment) + ` } | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
`
}

// adminInstallShell is the root shell command (macOS admin dialog) that
// installs a staged file only if its bytes still hash to sha256Hex. Root
// copies it into the destination folder first and checks that copy, which no
// other user can change, so the check and the install see the same bytes.
// Only quoted values reach the shell.
func adminInstallShell(staged, sha256Hex, dest string) string {
	dir := path.Dir(dest)
	return "/bin/mkdir -p " + ShellQuote(dir) +
		` && t=$(/usr/bin/mktemp ` + ShellQuote(dir+"/.hull.XXXXXX") + `)` +
		` && { /bin/cat ` + ShellQuote(staged) + ` > "$t"` +
		` && [ "$(/usr/bin/shasum -a 256 < "$t" | /usr/bin/cut -d ' ' -f 1)" = ` + ShellQuote(sha256Hex) + ` ]` +
		` && /bin/chmod 0644 "$t" && /bin/mv -f "$t" ` + ShellQuote(dest) +
		` || { /bin/rm -f "$t"; exit 1; }; }`
}

// sudoStep is one privileged command run through sudo as argv (no shell),
// with stdin feeding it (the content for tee).
type sudoStep struct {
	stdin string
	args  []string
}

func resolvedSteps(dir, path, content string) []sudoStep {
	return []sudoStep{
		{args: []string{"mkdir", "-p", dir}},
		{stdin: content, args: []string{"tee", path}},
		{args: []string{"systemctl", "restart", "systemd-resolved"}},
	}
}

func nmSteps(confDir, dnsmasqDir, confPath, rulePath, rule string) []sudoStep {
	return []sudoStep{
		{args: []string{"mkdir", "-p", confDir, dnsmasqDir}},
		{stdin: "[main]\ndns=dnsmasq\n", args: []string{"tee", confPath}},
		{stdin: rule, args: []string{"tee", rulePath}},
		{args: []string{"systemctl", "restart", "NetworkManager"}},
	}
}
