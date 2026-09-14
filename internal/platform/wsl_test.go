package platform

import (
	"strings"
	"testing"
)

// The UNC spelling is not cosmetic. Docker mounts an EMPTY directory, silently
// and successfully, for the forward-slash form, so a renderer that produced it
// would serve an empty site rather than report a problem.
func TestUNCPath(t *testing.T) {
	for _, tc := range []struct {
		distro, linux, want string
	}{
		{"Debian", "/home/me/Sites/app", `\\wsl.localhost\Debian\home\me\Sites\app`},
		{"Ubuntu-22.04", "/srv/x", `\\wsl.localhost\Ubuntu-22.04\srv\x`},
		{"Debian", "home/me", `\\wsl.localhost\Debian\home\me`}, // tolerates a missing leading slash
	} {
		if got := UNCPath(tc.distro, tc.linux); got != tc.want {
			t.Errorf("UNCPath(%q, %q) = %q, want %q", tc.distro, tc.linux, got, tc.want)
		}
	}
	if got := UNCPath("Debian", "/x"); strings.Contains(got, "/") {
		t.Errorf("UNCPath must use backslashes only, got %q", got)
	}
}

func TestOnWSLFilesystem(t *testing.T) {
	yes := []string{
		`\\wsl.localhost\Debian\home\me\app`,
		`\\WSL.LOCALHOST\Debian\home\me`,
		`\\wsl$\Ubuntu\home\me\app`,
		`//wsl.localhost/Debian/home/me`,
	}
	for _, p := range yes {
		if !OnWSLFilesystem(p) {
			t.Errorf("OnWSLFilesystem(%q) = false, want true", p)
		}
	}
	no := []string{`W:\Sites\app`, `/home/me/app`, `\\server\share\app`, "", `/mnt/c/Sites`}
	for _, p := range no {
		if OnWSLFilesystem(p) {
			t.Errorf("OnWSLFilesystem(%q) = true, want false", p)
		}
	}
}

// A project inside WSL must NOT be treated as being on the slow Windows
// filesystem: it is reached through a different mechanism entirely and measures
// as fast as the container's own disk. Getting this backwards would mount the
// watched-reload ini on a project that does not need it.
func TestWSLPathIsNotWindowsFilesystem(t *testing.T) {
	p := `\\wsl.localhost\Debian\home\me\Sites\app`
	if OnWindowsFilesystem(p) {
		t.Errorf("OnWindowsFilesystem(%q) = true; a WSL path is not the slow 9p mount", p)
	}
}

func TestWSLDistroOf(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`\\wsl.localhost\Debian\home\me`, "Debian"},
		{`\\wsl$\Ubuntu-22.04\home`, "Ubuntu-22.04"},
		{`\\wsl.localhost\Debian`, "Debian"},
		{`W:\Sites\app`, ""},
		{`/home/me`, ""},
	} {
		if got := WSLDistroOf(tc.in); got != tc.want {
			t.Errorf("WSLDistroOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWSLLinuxPath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`\\wsl.localhost\Debian\home\me\Sites\app`, "/home/me/Sites/app"},
		{`\\wsl$\Ubuntu\srv\x`, "/srv/x"},
		{`W:\Sites\app`, "/mnt/w/Sites/app"},
		{`C:\Users\me\Work`, "/mnt/c/Users/me/Work"},
		{`/home/me/app`, ""}, // already a Linux path, and not one WSL exposes
		{``, ""},
	} {
		if got := WSLLinuxPath(tc.in); got != tc.want {
			t.Errorf("WSLLinuxPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// wsl.exe writes UTF-16LE, so a raw read of "Debian" arrives as "D\0e\0b\0..."
// and every naive comparison against it fails.
func TestDecodeWSL(t *testing.T) {
	utf16le := []byte{0xFF, 0xFE, 'D', 0, 'e', 0, 'b', 0, 'i', 0, 'a', 0, 'n', 0, '\r', 0, '\n', 0}
	if got := decodeWSL(utf16le); got != "Debian\n" {
		t.Errorf("decodeWSL(utf16 with BOM) = %q, want %q", got, "Debian\n")
	}
	noBOM := []byte{'D', 0, 'e', 0, 'b', 0, 'i', 0, 'a', 0, 'n', 0}
	if got := decodeWSL(noBOM); got != "Debian" {
		t.Errorf("decodeWSL(utf16 without BOM) = %q, want %q", got, "Debian")
	}
	// Output from inside a distro is already UTF-8 and must survive untouched.
	if got := decodeWSL([]byte("/home/me\r\n")); got != "/home/me\n" {
		t.Errorf("decodeWSL(utf8) = %q, want %q", got, "/home/me\n")
	}
	if got := decodeWSL(nil); got != "" {
		t.Errorf("decodeWSL(nil) = %q, want empty", got)
	}
}

func TestIsDockerDistro(t *testing.T) {
	for _, n := range []string{"docker-desktop", "docker-desktop-data", "Docker-Desktop"} {
		if !isDockerDistro(n) {
			t.Errorf("isDockerDistro(%q) = false; Docker's own distros are not somewhere to keep code", n)
		}
	}
	for _, n := range []string{"Debian", "Ubuntu", "docker"} {
		if isDockerDistro(n) {
			t.Errorf("isDockerDistro(%q) = true, want false", n)
		}
	}
}
