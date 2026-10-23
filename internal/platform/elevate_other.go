//go:build !windows

package platform

// MaybeRunElevatedHelper is Windows-only (hull.exe as its own UAC helper).
func MaybeRunElevatedHelper() bool { return false }
