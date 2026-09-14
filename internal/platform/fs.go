package platform

import (
	"path/filepath"
	"strings"
)

// OnWindowsFilesystem reports whether a path lives on the Windows filesystem.
//
// This matters because Docker serves such a path to containers over a 9p mount
// where every file operation costs milliseconds rather than microseconds, so
// anything that touches thousands of files (PHP loading a framework) takes
// seconds. Hull uses it both to warn at project creation and to decide whether
// a project needs the watched-reload treatment.
//
// It covers native Windows drive paths (C:\...) and the Windows drives exposed
// inside WSL (/mnt/c/...), which are the same slow mount seen from the other
// side.
//
// A \\wsl.localhost\<distro>\... path is deliberately NOT matched, and that is
// not an oversight to be tidied up later. Docker reaches such a path through the
// distro's own mount service rather than the 9p share, and it measures as fast
// as the container's own disk, so a project there needs none of the mitigations
// this predicate switches on. Use OnWSLFilesystem to ask the opposite question.
func OnWindowsFilesystem(root string) bool {
	if vol := filepath.VolumeName(root); len(vol) == 2 && vol[1] == ':' {
		return true // C:\ on native Windows
	}
	r := filepath.ToSlash(root)
	if len(r) >= 7 && strings.HasPrefix(r, "/mnt/") && r[6] == '/' {
		c := r[5]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}
