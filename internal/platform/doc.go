// Package platform isolates per-OS integration: DNS registration
// (systemd-resolved drop-ins, /etc/resolver files, Windows NRPT rules
// with hosts-file fallback), trust-store paths, daemon service install
// (systemd/launchd/Windows service), elevation flows, and WSL2
// detection. All build-tagged per GOOS. (Phase 4)
package platform
