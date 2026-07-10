//go:build !linux

package platform

// RestartDaemonService is a no-op off Linux, where Hull has no systemd --user
// service to bounce; the caller advises a manual daemon restart when needed.
func RestartDaemonService() (bool, error) { return false, nil }
