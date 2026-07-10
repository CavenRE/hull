//go:build linux

package platform

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// unprivilegedPortStart returns net.ipv4.ip_unprivileged_port_start , ports at
// or above it bind without privilege. Defaults to the kernel default (1024)
// when the sysctl can't be read.
func unprivilegedPortStart() int {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start")
	if err != nil {
		return 1024
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n < 0 {
		return 1024
	}
	return n
}

// EnsurePortBind grants the running executable CAP_NET_BIND_SERVICE when any of
// ports is privileged on this kernel (below ip_unprivileged_port_start), so the
// daemon's embedded router can bind them. It is a deliberate no-op when every
// port is already unprivileged (e.g. a box with ip_unprivileged_port_start=80),
// so it never triggers a needless pkexec prompt. The capability is a file
// attribute on the binary, so it takes effect for the NEXT daemon start, not
// the current process. Returns a human status line, or a ManualStepsError when
// elevation is needed but unavailable.
func EnsurePortBind(ports []int) (string, error) {
	start := unprivilegedPortStart()
	var privileged []int
	for _, p := range ports {
		if p < start {
			privileged = append(privileged, p)
		}
	}
	if len(privileged) == 0 {
		return "port(s) " + joinInts(ports) + " bind without elevation (ip_unprivileged_port_start=" + strconv.Itoa(start) + ")", nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if hasPortBindCap(exe) {
		return "CAP_NET_BIND_SERVICE already present on " + exe, nil
	}
	if err := GrantPortBind(exe); err != nil {
		return "", err
	}
	return "granted CAP_NET_BIND_SERVICE to " + exe + " for privileged port(s) " + joinInts(privileged), nil
}

// hasPortBindCap reports whether exe already carries cap_net_bind_service, so a
// second `hull setup` (or a prior install.sh setcap) doesn't re-prompt.
func hasPortBindCap(exe string) bool {
	out, err := exec.Command("getcap", exe).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "cap_net_bind_service")
}

func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, ",")
}
