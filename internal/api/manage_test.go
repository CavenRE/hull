package api

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	s, client, _ := testServer(t)

	var cfg ConfigInfo
	if err := client.do(context.Background(), http.MethodGet, "/v1/config", nil, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.TLD != "test" || len(cfg.Roots) != 1 {
		t.Fatalf("initial config = %+v", cfg)
	}

	newRoot := t.TempDir()
	cfg.Roots = append(cfg.Roots, newRoot)
	cfg.Defaults.Editor = "code"
	cfg.Defaults.DBTool = "tableplus"
	var resp ConfigInfo
	if err := client.do(context.Background(), http.MethodPut, "/v1/config", cfg, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Roots) != 2 || resp.Defaults.Editor != "code" {
		t.Fatalf("updated config = %+v", resp)
	}
	if len(resp.RestartRequired) != 0 {
		t.Errorf("no restart should be needed: %v", resp.RestartRequired)
	}

	// Persisted to disk.
	data, err := os.ReadFile(filepath.Join(s.Config.HullHome, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "editor: code") || !strings.Contains(string(data), "db_tool: tableplus") {
		t.Errorf("config.yaml:\n%s", data)
	}

	// TLD change flags a restart.
	cfg.TLD = "dev"
	if err := client.do(context.Background(), http.MethodPut, "/v1/config", cfg, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.RestartRequired) != 1 || resp.RestartRequired[0] != "tld" {
		t.Errorf("restart_required = %v", resp.RestartRequired)
	}

	// Empty roots rejected.
	cfg.Roots = nil
	if err := client.do(context.Background(), http.MethodPut, "/v1/config", cfg, &resp); err == nil {
		t.Error("empty roots should 400")
	}
}

func TestProjectOpen(t *testing.T) {
	s, client, rec := testServer(t)
	writeProject(t, s.Config.Roots[0], "alpha")

	if err := client.do(context.Background(), http.MethodPost, "/v1/projects/alpha/open", OpenRequest{Target: "folder"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(rec.commands) != 1 || !strings.Contains(rec.commands[0], "alpha") {
		t.Errorf("commands = %v", rec.commands)
	}

	// Editor without a configured editor → 400.
	err := client.do(context.Background(), http.MethodPost, "/v1/projects/alpha/open", OpenRequest{Target: "editor"}, nil)
	if err == nil || !strings.Contains(err.Error(), "Settings") {
		t.Errorf("editor err = %v", err)
	}
	s.Config.Defaults.Editor = "code"
	if err := client.do(context.Background(), http.MethodPost, "/v1/projects/alpha/open", OpenRequest{Target: "editor"}, nil); err != nil {
		t.Fatal(err)
	}
	last := rec.commands[len(rec.commands)-1]
	if !strings.HasPrefix(last, "code ") {
		t.Errorf("editor command = %s", last)
	}
}

func TestProjectPatch(t *testing.T) {
	s, client, _ := testServer(t)
	dir := filepath.Join(s.Config.Roots[0], "shop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema: 1\nname: shop\ntemplate: laravel\nphp: \"8.2\"\n"
	if err := os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	php := "8.4"
	if err := client.do(context.Background(), http.MethodPatch, "/v1/projects/shop", PatchProjectRequest{PHP: &php}, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "hull.yaml"))
	if !strings.Contains(string(data), `php: "8.4"`) {
		t.Errorf("manifest after patch:\n%s", data)
	}
	composeData, _ := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if !strings.Contains(string(composeData), "php:8.4-fpm-nginx") {
		t.Errorf("compose not re-rendered:\n%s", composeData)
	}

	// Invalid php rejected, manifest untouched.
	bad := "latest"
	err := client.do(context.Background(), http.MethodPatch, "/v1/projects/shop", PatchProjectRequest{PHP: &bad}, nil)
	if err == nil {
		t.Error("invalid php should 400")
	}
	data, _ = os.ReadFile(filepath.Join(dir, "hull.yaml"))
	if !strings.Contains(string(data), `php: "8.4"`) {
		t.Errorf("manifest corrupted by failed patch:\n%s", data)
	}
}

func TestLogsSSE(t *testing.T) {
	s, client, _ := testServer(t)
	writeProject(t, s.Config.Roots[0], "alpha")
	s.LogStream = func(ctx context.Context, dir string, tail int, onLine func(string)) error {
		if tail != 50 {
			t.Errorf("tail = %d", tail)
		}
		onLine("line one")
		onLine("line two")
		return nil
	}

	req, _ := http.NewRequest(http.MethodGet, client.BaseURL+"/v1/logs?project=alpha&tail=50", nil)
	req.Header.Set("Authorization", "Bearer "+client.Token)
	resp, err := client.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var got []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, "data: ") {
			got = append(got, strings.TrimPrefix(line, "data: "))
		}
	}
	if len(got) != 2 || got[0] != "line one" || got[1] != "line two" {
		t.Errorf("sse lines = %v", got)
	}

	// Missing source → 400.
	req2, _ := http.NewRequest(http.MethodGet, client.BaseURL+"/v1/logs", nil)
	req2.Header.Set("Authorization", "Bearer "+client.Token)
	resp2, err := client.HTTP.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("no-source status = %d", resp2.StatusCode)
	}
}

func TestDoctorEndpoint(t *testing.T) {
	_, client, _ := testServer(t)
	var checks []map[string]any
	if err := client.do(context.Background(), http.MethodGet, "/v1/doctor", nil, &checks); err != nil {
		t.Fatal(err)
	}
	if len(checks) == 0 {
		t.Fatal("no checks returned")
	}
	found := false
	for _, c := range checks {
		if c["name"] == "daemon" && c["status"] == "ok" {
			found = true
		}
	}
	if !found {
		t.Errorf("daemon check missing/not ok: %v", checks)
	}
}
