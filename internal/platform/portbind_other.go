//go:build !linux

package platform

// EnsurePortBind is a no-op off Linux. Windows has no CAP_NET_BIND_SERVICE
// model (and lets processes bind low ports), and macOS setup takes a different
// privilege path, so there is nothing to grant here.
func EnsurePortBind(ports []int) (string, error) {
	_ = ports
	return "", nil
}

// PortBindCapable is false off Linux, which has no file capabilities.
func PortBindCapable(string) bool { return false }

// GrantPortBind is a no-op off Linux.
func GrantPortBind(string) error { return nil }

// WritableCapped is false off Linux.
func WritableCapped(string) bool { return false }
