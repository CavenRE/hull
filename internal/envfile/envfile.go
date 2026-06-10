// Package envfile edits dotenv-style files with Hull v1's set_env
// semantics, preserving unrelated lines, comments, and ordering.
package envfile

import (
	"os"
	"strings"
)

// Set returns content with key assigned to value. Matching v1 semantics:
// every live "KEY=..." line is replaced; otherwise every commented
// "# KEY=..." line is replaced (uncommented); otherwise the assignment is
// appended. The file's dominant line ending (LF or CRLF) is preserved.
func Set(content, key, value string) string {
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	lines := splitLines(content)
	assignment := key + "=" + value

	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = assignment
			replaced = true
		}
	}
	if !replaced {
		for i, line := range lines {
			if strings.HasPrefix(line, "# "+key+"=") {
				lines[i] = assignment
				replaced = true
			}
		}
	}
	if !replaced {
		// Drop a single trailing blank produced by a final newline so the
		// assignment lands on its own line at the end of the file.
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		lines = append(lines, assignment)
	}

	out := strings.Join(lines, eol)
	if !strings.HasSuffix(out, eol) {
		out += eol
	}
	return out
}

// Get returns the value of the first live "KEY=..." line.
func Get(content, key string) (string, bool) {
	for _, line := range splitLines(content) {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// SetFile applies Set to the file at path, creating it if missing.
func SetFile(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(Set(string(data), key, value)), 0o644)
}

// splitLines splits on LF and strips any trailing CR, so callers always see
// clean line content regardless of the file's endings.
func splitLines(content string) []string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}
