//go:build !windows

package platform

// RestrictToUser is a no-op off Windows, where the file mode already does it.
func RestrictToUser(string) error { return nil }
