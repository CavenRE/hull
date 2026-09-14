package platform

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// This file is what Hull knows about WSL2, which on Windows is the difference
// between a PHP page taking five seconds and taking under one.
//
// The reason: Docker Desktop runs its engine inside a WSL2 VM. A project on a
// Windows drive reaches that engine over a 9p mount where a single file
// operation costs milliseconds, so a framework request touching thousands of
// files spends seconds in the filesystem. The same files inside the WSL Linux
// filesystem are served through Docker Desktop's per-distro mount service
// instead, which is ext4 straight through. Measured on a 1,059 file WordPress
// tree: 4,450 ms to read it over 9p, 10 ms from the distro, and 20 ms from a
// path inside the container itself. The distro is not merely faster, it is
// indistinguishable from container-native.
//
// The important discovery is how little that costs Hull. Docker Desktop accepts
// a \\wsl.localhost\<distro>\... bind mount from a Windows-side compose file,
// and Compose resolves a relative "./" mount against the directory the compose
// file sits in, so a project that simply lives at such a path needs no change
// to the renderer at all. Hull keeps running on Windows, owning DNS, the
// router and the ports; only the files move.

// Two timeouts, because the two kinds of call cost very different amounts.
// Listing distributions never starts one, so it must be quick or something is
// wrong. Running a command INSIDE a distribution boots it if it is not already
// up, and a cold boot takes tens of seconds on an ordinary machine; timing that
// out at ten would report "could not work out which user Debian runs as" for
// what is really just a distro waking up.
const (
	wslListTimeout  = 10 * time.Second
	wslEnterTimeout = 90 * time.Second
)

// WSLAvailable reports whether this machine can run WSL distributions at all.
func WSLAvailable() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	_, err := exec.LookPath("wsl.exe")
	return err == nil
}

// WSLDistros lists the WSL distributions worth putting a project in, newest
// listing order preserved. Docker Desktop's own utility distros are filtered
// out: they are implementation, not somewhere to keep your code.
func WSLDistros(ctx context.Context) []string {
	if !WSLAvailable() {
		return nil
	}
	out, err := wslOutput(ctx, wslListTimeout, "-l", "-q")
	if err != nil {
		return nil
	}
	var distros []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || isDockerDistro(name) {
			continue
		}
		distros = append(distros, name)
	}
	return distros
}

// isDockerDistro reports whether a distro name is one Docker Desktop manages.
func isDockerDistro(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "docker-desktop")
}

// WSLDockerIntegration reports whether Docker Desktop's WSL integration is
// enabled for a distro. That switch is what creates the per-distro mount
// service, so without it a \\wsl.localhost\<distro>\... bind mount fails with
// "accessing specified distro mount service" rather than being merely slow.
//
// It is checked functionally, by asking the distro whether it can reach the
// engine, because the setting itself is absent from Docker Desktop's settings
// file while it sits at its default.
func WSLDockerIntegration(ctx context.Context, distro string) bool {
	if !WSLAvailable() || distro == "" {
		return false
	}
	out, err := wslOutput(ctx, wslEnterTimeout, "-d", distro, "--", "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return false
	}
	// Without integration the Docker-provided shim on the Windows PATH answers
	// with a refusal rather than failing, so an exit code alone is not enough.
	return strings.TrimSpace(out) != "" && !strings.Contains(out, "could not be found in this WSL")
}

// WSLHome returns a distro's default user home directory (a Linux path).
func WSLHome(ctx context.Context, distro string) (string, error) {
	if !WSLAvailable() {
		return "", fmt.Errorf("WSL is not available on this machine")
	}
	out, err := wslOutput(ctx, wslEnterTimeout, "-d", distro, "--", "sh", "-c", "echo $HOME")
	if err != nil {
		return "", fmt.Errorf("asking %s for its home directory: %w", distro, err)
	}
	home := strings.TrimSpace(out)
	if home == "" || !strings.HasPrefix(home, "/") {
		return "", fmt.Errorf("%s reported an unusable home directory %q", distro, home)
	}
	return home, nil
}

// distroUsers caches the identity lookup. Render runs on every `hull up`, and
// spawning wsl.exe to ask the same question each time would put a distro boot
// on a hot path.
var (
	distroUserMu sync.Mutex
	distroUsers  = map[string][2]string{}
)

// WSLDistroUser returns the default user's uid and gid inside a distribution.
//
// This matters more than it looks. A Windows bind mount hides ownership: Docker
// Desktop presents every file as writable no matter who owns it, so a container
// running as www-data can write a project on W:\ without anyone thinking about
// uids. A WSL mount does not, because it is a real Linux filesystem: files
// copied into the distro belong to the distro user (typically 1000), and
// www-data (33) can read them but cannot write a single one. Uploads, caches,
// sessions and logs all fail.
//
// Hull already knows how to fix that, because native Linux has the same
// problem: it remaps the container's web user to the host identity. Moving a
// project into WSL means turning that machinery on for a Windows machine, which
// needs this number.
func WSLDistroUser(ctx context.Context, distro string) (uid, gid string, err error) {
	distroUserMu.Lock()
	if ids, ok := distroUsers[distro]; ok {
		distroUserMu.Unlock()
		return ids[0], ids[1], nil
	}
	distroUserMu.Unlock()

	out, err := wslOutput(ctx, wslEnterTimeout, "-d", distro, "--", "sh", "-c", "id -u; id -g")
	if err != nil {
		return "", "", fmt.Errorf("asking %s who it runs as: %w", distro, err)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return "", "", fmt.Errorf("%s reported an unreadable identity %q", distro, strings.TrimSpace(out))
	}
	uid, gid = fields[0], fields[1]
	for _, v := range []string{uid, gid} {
		if _, convErr := strconv.Atoi(v); convErr != nil {
			return "", "", fmt.Errorf("%s reported a non-numeric identity %q", distro, strings.TrimSpace(out))
		}
	}
	distroUserMu.Lock()
	distroUsers[distro] = [2]string{uid, gid}
	distroUserMu.Unlock()
	return uid, gid, nil
}

// UNCPath renders a Linux path inside a distro as the Windows path that reaches
// it. This is the spelling Hull stores and hands to Docker: the wsl.localhost
// form rather than the older wsl$ form, and backslashes rather than forward
// slashes, because Docker silently mounts an EMPTY directory for the
// forward-slash spelling instead of reporting an error.
func UNCPath(distro, linuxPath string) string {
	p := strings.ReplaceAll(strings.TrimPrefix(linuxPath, "/"), "/", `\`)
	return `\\wsl.localhost\` + distro + `\` + p
}

// These helpers parse Windows path syntax on every platform, using their own
// normalisation rather than the filepath package. That is deliberate:
// filepath.ToSlash leaves backslashes alone off Windows and
// filepath.VolumeName always returns "" there, so the same string would be read
// two different ways depending on where Hull happens to be running, and the
// tests would only hold on one of them. A WSL path means the same thing
// wherever it is read.

// normalizeWinPath makes a path comparable regardless of separator style.
func normalizeWinPath(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// driveLetter returns the drive letter of a path like "W:\Sites", lowercased,
// or "" when the path does not start with one.
func driveLetter(p string) string {
	if len(p) < 2 || p[1] != ':' {
		return ""
	}
	c := p[0]
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return strings.ToLower(string(c))
	}
	return ""
}

// OnWSLFilesystem reports whether a Windows path points into a WSL
// distribution, which is the fast place for a project to live.
func OnWSLFilesystem(p string) bool {
	s := strings.ToLower(normalizeWinPath(p))
	return strings.HasPrefix(s, "//wsl.localhost/") || strings.HasPrefix(s, "//wsl$/")
}

// WSLDistroOf returns the distribution a UNC path belongs to, or "" when the
// path is not a WSL path.
func WSLDistroOf(p string) string {
	s := normalizeWinPath(p)
	for _, prefix := range []string{"//wsl.localhost/", "//wsl$/"} {
		if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
			rest := s[len(prefix):]
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				return rest[:i]
			}
			return rest
		}
	}
	return ""
}

// WSLLinuxPath converts a Windows path to the path a WSL distro sees, so Hull
// can hand work to the distro's own tools (which read and write ext4 directly
// and are therefore far faster than copying across the boundary from Windows).
// A drive path becomes /mnt/<drive>/..., and a UNC path into a distro becomes
// the plain Linux path it already is. Returns "" when neither applies.
func WSLLinuxPath(p string) string {
	if WSLDistroOf(p) != "" {
		s := normalizeWinPath(p)
		host := strings.Index(s[2:], "/") // end of the //server component
		if host < 0 {
			return ""
		}
		rest := s[2+host:] // "/<distro>/home/..."
		distro := strings.Index(rest[1:], "/")
		if distro < 0 {
			return "/" // the distro root itself
		}
		return rest[1+distro:]
	}
	if drive := driveLetter(p); drive != "" {
		return "/mnt/" + drive + normalizeWinPath(p[2:])
	}
	return ""
}

// wslRunTimeout bounds a job handed to a distro. Copying a project is the only
// caller, and a large WordPress tree read across the filesystem boundary can
// genuinely take minutes, so this is generous where the diagnostics above are
// not.
const wslRunTimeout = 30 * time.Minute

// WSLRun executes a shell script inside a distribution and returns its combined
// output. Used to hand file work to the distro, whose own tools write ext4
// directly and are far faster at it than anything reaching in from Windows.
func WSLRun(ctx context.Context, distro, script string) (string, error) {
	if !WSLAvailable() {
		return "", fmt.Errorf("WSL is not available on this machine")
	}
	ctx, cancel := context.WithTimeout(ctx, wslRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "sh", "-c", script)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	return decodeWSL(out), err
}

// wslOutput runs wsl.exe and returns its stdout as UTF-8.
//
// wsl.exe writes UTF-16LE, so its output has to be decoded rather than read as
// bytes: a raw read of "Debian" comes back as "D e b i a n" with NUL bytes
// between the letters, which every naive comparison then fails.
func wslOutput(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return decodeWSL(out), err
	}
	return decodeWSL(out), nil
}

// decodeWSL turns wsl.exe's UTF-16LE output into a normal Go string, leaving
// output that is already UTF-8 (some subcommands, and anything run inside a
// distro) untouched.
func decodeWSL(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:] // strip the byte-order mark
	} else if !looksUTF16(b) {
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return strings.ReplaceAll(string(utf16.Decode(u)), "\r\n", "\n")
}

// looksUTF16 guesses the encoding of wsl.exe output that carries no byte-order
// mark, by looking for the NUL high bytes that ASCII text in UTF-16LE produces.
func looksUTF16(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	n := len(b)
	if n > 64 {
		n = 64
	}
	return bytes.Count(b[:n], []byte{0}) > n/4
}
