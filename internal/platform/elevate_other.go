//go:build !windows

package platform

import "os"

// MaybeRunElevatedHelper is Windows-only (hull.exe as its own UAC helper).
func MaybeRunElevatedHelper() bool { return false }

// LinkDir makes link a directory symlink to target.
func LinkDir(link, target string) error { return os.Symlink(target, link) }
