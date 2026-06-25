package platform

import (
	"os"
	"sort"
	"strings"
)

// ManagedHostsCount returns how many host entries Hull currently manages in
// the OS hosts file (0 if no block or unreadable). On the hosts-file
// strategy (Windows) this is how browsers resolve *.tld names.
func ManagedHostsCount() int {
	data, err := os.ReadFile(hostsFilePath())
	if err != nil {
		return 0
	}
	count, inBlock := 0, false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == HostsBegin:
			inBlock = true
		case trimmed == HostsEnd:
			inBlock = false
		case inBlock && strings.HasPrefix(trimmed, "127."):
			count += len(strings.Fields(trimmed)) - 1 // names after the IP
		}
	}
	return count
}

// Hosts-block markers, Herd-style: Hull owns exactly this region of the
// hosts file and never touches anything outside it.
const (
	HostsBegin = "# Hull generated Hosts. Do not change."
	HostsEnd   = "# End Hull generated Hosts"
)

// HostsBlock renders Hull's managed block: one line per domain pointing at the
// router's loopback (e.g. 127.0.0.2), sorted. Empty domains means no block at
// all; an empty ip falls back to 127.0.0.1.
func HostsBlock(domains []string, ip string) string {
	if len(domains) == 0 {
		return ""
	}
	if ip == "" {
		ip = "127.0.0.1"
	}
	sorted := append([]string(nil), domains...)
	sort.Strings(sorted)
	var sb strings.Builder
	sb.WriteString(HostsBegin)
	sb.WriteString("\n")
	for _, d := range sorted {
		sb.WriteString(ip + " ")
		sb.WriteString(d)
		sb.WriteString("\n")
	}
	sb.WriteString(HostsEnd)
	return sb.String()
}

// MergeHostsBlock returns the hosts-file content with Hull's block
// replaced (or appended, or removed when block is empty). Line endings of
// the result are normalized to CRLF for the Windows hosts file; all
// non-Hull content is preserved byte-for-byte apart from that.
func MergeHostsBlock(current, block string) string {
	normalized := strings.ReplaceAll(current, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	var out []string
	inBlock := false
	replaced := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == HostsBegin:
			inBlock = true
			replaced = true
			if block != "" {
				out = append(out, strings.Split(block, "\n")...)
			}
		case inBlock && trimmed == HostsEnd:
			inBlock = false
		case inBlock:
			// old block content, dropped
		default:
			out = append(out, line)
		}
	}
	if !replaced && block != "" {
		// Trim trailing blank lines, then append with a separating blank.
		for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
		out = append(out, "", "")
		out[len(out)-1] = block
	}

	result := strings.Join(out, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return strings.ReplaceAll(result, "\n", "\r\n")
}
