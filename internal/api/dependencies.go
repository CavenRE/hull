package api

import (
	"context"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
)

// DependencyInfo describes one external/embedded dependency for the GUI's
// Updates panel and the first-run wizard's system check.
type DependencyInfo struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Installed  bool   `json:"installed"`
	Version    string `json:"version,omitempty"`
	Running    bool   `json:"running"`
	Status     string `json:"status"` // ok | stopped | missing | embedded
	Blurb      string `json:"blurb"`
	InstallURL string `json:"install_url,omitempty"`
	// InstallHint is a copy-paste install command (Linux package managers).
	InstallHint string `json:"install_hint,omitempty"`
	// Embedded: built into the Hull daemon, not separately installable.
	Embedded bool `json:"embedded,omitempty"`
}

func (s *Server) registerDependencyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/dependencies", s.handleDependencies)
}

func (s *Server) handleDependencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, DetectDependencies(r.Context(), s.Config.TLD))
}

// DetectDependencies probes Docker and reports the embedded components.
// Standalone so the CLI can use it in-process (no daemon required).
func DetectDependencies(ctx context.Context, tld string) []DependencyInfo {
	docker := DependencyInfo{
		Name:        "Docker Engine",
		Key:         "docker",
		Blurb:       "Container runtime , Hull's one external dependency.",
		InstallURL:  dockerInstallURL(),
		InstallHint: dockerInstallHint(),
	}
	if _, err := exec.LookPath("docker"); err != nil {
		docker.Status = "missing"
	} else {
		docker.Installed = true
		if v, err := dockerOutput(ctx, "version", "--format", "{{.Client.Version}}"); err == nil && v != "" {
			docker.Version = v
		}
		if _, err := dockerOutput(ctx, "version", "--format", "{{.Server.Version}}"); err == nil {
			docker.Running = true
			docker.Status = "ok"
		} else {
			docker.Status = "stopped"
		}
	}

	embedded := func(name, key, blurb string) DependencyInfo {
		return DependencyInfo{Name: name, Key: key, Blurb: blurb, Installed: true, Running: true, Status: "embedded", Embedded: true, Version: "built-in"}
	}
	return []DependencyInfo{
		docker,
		embedded("Caddy router", "router", "HTTPS routing for *."+tld+", built into hulld."),
		embedded("Local DNS", "dns", "Wildcard *."+tld+" resolver, built into hulld."),
		embedded("Local CA", "ca", "Issues + trusts local TLS certs, built into hulld."),
	}
}

func dockerOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	return strings.TrimSpace(string(out)), err
}

func dockerInstallURL() string {
	switch runtime.GOOS {
	case "windows", "darwin":
		return "https://www.docker.com/products/docker-desktop/"
	default:
		return "https://docs.docker.com/engine/install/"
	}
}

// dockerInstallHint returns a copy-paste install command on Linux distros we
// can recognize; empty elsewhere (Windows/macOS use the download installer).
func dockerInstallHint() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	switch linuxDistro() {
	case "arch", "manjaro", "endeavouros":
		return "sudo pacman -S docker docker-compose && sudo systemctl enable --now docker"
	case "debian", "ubuntu", "linuxmint", "pop":
		return "curl -fsSL https://get.docker.com | sh && sudo systemctl enable --now docker"
	default:
		return ""
	}
}

func linuxDistro() string {
	data, err := exec.Command("sh", "-c", ". /etc/os-release 2>/dev/null && echo $ID").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
