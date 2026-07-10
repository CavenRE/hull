//go:build !linux

package platform

// EnsurePortBind is a no-op off Linux. Windows has no CAP_NET_BIND_SERVICE
// model (and lets processes bind low ports), and macOS setup takes a different
// privilege path, so there is nothing to grant here.
func EnsurePortBind(ports []int) (string, error) {
	_ = ports
	return "", nil
}
