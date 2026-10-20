package platform

import (
	"net"
	"runtime"
)

// LoopbackPresent reports whether ip can be bound here. Linux and Windows
// answer for all of 127.0.0.0/8; macOS only has 127.0.0.1 on lo0 unless an
// alias was added.
func LoopbackPresent(ip string) bool {
	if runtime.GOOS != "darwin" || ip == "127.0.0.1" {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.String() == ip {
			return true
		}
	}
	return false
}
