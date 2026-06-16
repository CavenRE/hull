package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
	"github.com/CavenRE/hull/internal/engine"
	"github.com/CavenRE/hull/internal/jobs"
	"github.com/CavenRE/hull/internal/services"
)

type recorded struct {
	commands []string
}

func (r *recorded) runner() dockerx.Runner {
	return func(ctx context.Context, dir, name string, args ...string) error {
		r.commands = append(r.commands, name+" "+strings.Join(args, " "))
		return nil
	}
}

func testServer(t *testing.T) (*Server, *Client, *recorded) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{TLD: "test", Roots: []string{root}, HullHome: t.TempDir()}
	rec := &recorded{}
	s := NewServer(cfg, "secret-token")
	s.Engine = engine.New(cfg)
	s.Engine.Run = rec.runner()
	s.Engine.EnsureNet = func(ctx context.Context, name string) error { return nil }
	s.NewJobEngine = func(log func(string)) *engine.Engine {
		e := engine.New(cfg)
		e.Run = func(ctx context.Context, dir, name string, args ...string) error {
			log("$ " + name + " " + strings.Join(args, " "))
			rec.commands = append(rec.commands, name+" "+strings.Join(args, " "))
			return nil
		}
		e.EnsureNet = func(ctx context.Context, name string) error { return nil }
		return e
	}
	fakeServices := func() *services.Manager {
		return &services.Manager{
			HullHome: cfg.HullHome,
			Run:      rec.runner(),
			Output: func(ctx context.Context, dir, name string, args ...string) (string, error) {
				rec.commands = append(rec.commands, name+" "+strings.Join(args, " "))
				return "", nil
			},
			EnsureNet:       func(ctx context.Context, name string) error { return nil },
			RunningProjects: func(ctx context.Context) ([]string, error) { return []string{"hull-mailpit"}, nil },
		}
	}
	s.Services = fakeServices
	s.JobServices = func(log func(string)) *services.Manager { return fakeServices() }
	s.RunningProjects = func(ctx context.Context) ([]string, error) { return []string{"alpha"}, nil }

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	client := &Client{BaseURL: ts.URL, Token: "secret-token", HTTP: ts.Client()}
	return s, client, rec
}

func writeProject(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema: 1\nname: " + name + "\ntemplate: plain\n"
	if err := os.WriteFile(filepath.Join(dir, "hull.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAuthRequired(t *testing.T) {
	_, client, _ := testServer(t)
	bad := &Client{BaseURL: client.BaseURL, Token: "wrong", HTTP: client.HTTP}
	if _, err := bad.Status(context.Background()); err == nil {
		t.Fatal("wrong token should be rejected")
	}
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("correct token rejected: %v", err)
	}
}

func TestQueryTokenAndCORS(t *testing.T) {
	_, client, _ := testServer(t)

	// SSE-style query token authenticates.
	resp, err := client.HTTP.Get(client.BaseURL + "/v1/status?token=secret-token")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("query token status = %d", resp.StatusCode)
	}
	resp, err = client.HTTP.Get(client.BaseURL + "/v1/status?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong query token status = %d", resp.StatusCode)
	}

	// CORS preflight needs no token and reflects the origin.
	req, _ := http.NewRequest(http.MethodOptions, client.BaseURL+"/v1/projects", nil)
	req.Header.Set("Origin", "tauri://localhost")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err = client.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "tauri://localhost" {
		t.Errorf("allow-origin = %q", got)
	}
	// The webview uses PUT (config) and PATCH (project) — preflight must allow them.
	methods := resp.Header.Get("Access-Control-Allow-Methods")
	for _, m := range []string{"PUT", "PATCH", "DELETE"} {
		if !strings.Contains(methods, m) {
			t.Errorf("allow-methods %q missing %s", methods, m)
		}
	}
	if h := resp.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(h, "Authorization") {
		t.Errorf("allow-headers = %q", h)
	}
}

func TestStatus(t *testing.T) {
	s, client, _ := testServer(t)
	st, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.TLD != "test" || st.HullHome != s.Config.HullHome {
		t.Errorf("status = %+v", st)
	}
}

func TestProjectsAndRunningState(t *testing.T) {
	s, client, _ := testServer(t)
	writeProject(t, s.Config.Roots[0], "alpha")
	writeProject(t, s.Config.Roots[0], "beta")

	infos, err := client.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("projects = %+v", infos)
	}
	if !infos[0].Running || infos[0].Name != "alpha" {
		t.Errorf("alpha should be running: %+v", infos[0])
	}
	if infos[1].Running {
		t.Errorf("beta should be stopped: %+v", infos[1])
	}
	if infos[0].URL != "https://alpha.test" {
		t.Errorf("alpha url = %s", infos[0].URL)
	}
}

func TestProjectAction(t *testing.T) {
	s, client, rec := testServer(t)
	writeProject(t, s.Config.Roots[0], "alpha")

	if err := client.ProjectAction(context.Background(), "alpha", "start"); err != nil {
		t.Fatal(err)
	}
	if len(rec.commands) != 1 || rec.commands[0] != "docker compose up -d" {
		t.Errorf("commands = %v", rec.commands)
	}
	if err := client.ProjectAction(context.Background(), "ghost", "start"); err == nil {
		t.Error("missing project should 404")
	}
	if err := client.ProjectAction(context.Background(), "alpha", "explode"); err == nil {
		t.Error("unknown action should 404")
	}
}

func TestCreateProjectJob(t *testing.T) {
	s, client, _ := testServer(t)
	job, err := client.CreateProject(context.Background(), CreateProjectRequest{
		Name: "fresh", Template: "plain", SkipStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	final, err := client.WaitJob(context.Background(), job.ID, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != jobs.StatusDone {
		t.Fatalf("job = %+v (lines %v)", final, lines)
	}
	if _, err := os.Stat(filepath.Join(s.Config.Roots[0], "fresh", "hull.yaml")); err != nil {
		t.Error("hull.yaml not written by job")
	}
	if _, err := os.Stat(filepath.Join(s.Config.Roots[0], "fresh", "index.php")); err != nil {
		t.Error("plain scaffold not run by job")
	}
}

func TestCreateProjectJobFailure(t *testing.T) {
	_, client, _ := testServer(t)
	// "@@@" slugifies to empty → unsalvageable → the create job must fail.
	job, err := client.CreateProject(context.Background(), CreateProjectRequest{
		Name: "@@@", Template: "plain", SkipStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := client.WaitJob(context.Background(), job.ID, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != jobs.StatusFailed || final.Error == "" {
		t.Errorf("job = %+v", final)
	}
}

func TestImportUnmanagedFolder(t *testing.T) {
	s, client, _ := testServer(t)
	dir := filepath.Join(s.Config.Roots[0], "oldsite")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Listed as an unstarted folder before import.
	infos, err := client.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Kind != "folder" {
		t.Fatalf("pre-import listing = %+v", infos)
	}

	var ref JobRef
	if err := client.do(context.Background(), http.MethodPost, "/v1/imports", ImportRequest{Name: "oldsite"}, &ref); err != nil {
		t.Fatal(err)
	}
	final, err := client.WaitJob(context.Background(), ref.Job.ID, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != jobs.StatusDone {
		t.Fatalf("import job = %+v", final)
	}
	infos, err = client.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Kind != "plain" {
		t.Errorf("post-import listing = %+v", infos)
	}

	// Re-import must refuse.
	err = client.do(context.Background(), http.MethodPost, "/v1/imports", ImportRequest{Name: "oldsite"}, &ref)
	if err == nil {
		t.Error("second import should conflict")
	}
}

func TestServicesEndpoints(t *testing.T) {
	s, client, _ := testServer(t)

	// Empty at first.
	var infos []ServiceInfo
	if err := client.do(context.Background(), http.MethodGet, "/v1/services", nil, &infos); err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 {
		t.Fatalf("initial services = %+v", infos)
	}

	// Add mailpit as a job.
	var ref JobRef
	if err := client.do(context.Background(), http.MethodPost, "/v1/services", AddServiceRequest{Engine: "mailpit"}, &ref); err != nil {
		t.Fatal(err)
	}
	final, err := client.WaitJob(context.Background(), ref.Job.ID, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != jobs.StatusDone {
		t.Fatalf("add job = %+v", final)
	}

	if err := client.do(context.Background(), http.MethodGet, "/v1/services", nil, &infos); err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Name != "mailpit" || !infos[0].Running {
		t.Fatalf("services = %+v", infos)
	}
	if infos[0].URL != "https://mail.test" {
		t.Errorf("mailpit url = %q", infos[0].URL)
	}

	// Unknown engine rejected up front.
	err = client.do(context.Background(), http.MethodPost, "/v1/services", AddServiceRequest{Engine: "mongo"}, &ref)
	if err == nil {
		t.Error("unknown engine should 400")
	}

	// Link a project to it.
	writeProject(t, s.Config.Roots[0], "shop")
	if err := client.do(context.Background(), http.MethodPost, "/v1/services/mailpit/link", LinkRequest{Project: "shop"}, &ref); err != nil {
		t.Fatal(err)
	}
	final, err = client.WaitJob(context.Background(), ref.Job.ID, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != jobs.StatusDone {
		t.Fatalf("link job = %+v (lines %v)", final, final.Lines)
	}
	data, err := os.ReadFile(filepath.Join(s.Config.Roots[0], "shop", "hull.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mailpit") || !strings.Contains(string(data), "shared") {
		t.Errorf("manifest after link:\n%s", data)
	}

	// Stop and remove.
	if err := client.do(context.Background(), http.MethodPost, "/v1/services/mailpit/stop", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.do(context.Background(), http.MethodDelete, "/v1/services/mailpit", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.do(context.Background(), http.MethodGet, "/v1/services", nil, &infos); err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 {
		t.Fatalf("services after remove = %+v", infos)
	}
}

func TestJobStreamFinishedJob(t *testing.T) {
	s, client, _ := testServer(t)
	job := s.Jobs.Start("noop", func(log func(string)) error {
		log("hello")
		return nil
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if s := job.Snapshot(); s.Status != jobs.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}

	req, _ := http.NewRequest(http.MethodGet, client.BaseURL+"/v1/jobs/"+job.Snapshot().ID+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+client.Token)
	resp, err := client.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readAll(t, resp)
	if !strings.Contains(body, "data: hello") || !strings.Contains(body, "event: done") {
		t.Errorf("stream body:\n%s", body)
	}
}

func TestEventsFirstSnapshot(t *testing.T) {
	old := EventPollInterval
	EventPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { EventPollInterval = old })

	_, client, _ := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, client.BaseURL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+client.Token)
	resp, err := client.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, `"running":["alpha"]`) {
				t.Errorf("first event = %s", line)
			}
			return
		}
	}
	t.Fatal("no event received")
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestDaemonFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	info := DaemonInfo{Port: 12345, Token: "tok", PID: 99}
	if err := WriteDaemonFile(home, info); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDaemonFile(home)
	if err != nil {
		t.Fatal(err)
	}
	if *got != info {
		t.Errorf("round trip = %+v", got)
	}
	RemoveDaemonFile(home)
	if _, err := ReadDaemonFile(home); err == nil {
		t.Error("expected error after removal")
	}
}

func TestConnectNoDaemon(t *testing.T) {
	if _, ok := Connect(t.TempDir()); ok {
		t.Error("Connect should fail with no daemon file")
	}
}

func TestServeLifecycle(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{TLD: "test", Roots: []string{t.TempDir()}, HullHome: home}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, nil) }()

	var client *Client
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, ok := Connect(home); ok {
			client = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon never became reachable")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not exit after shutdown")
	}
	if _, err := ReadDaemonFile(home); err == nil {
		t.Error("daemon file not cleaned up")
	}
}
