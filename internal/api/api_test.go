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
	job, err := client.CreateProject(context.Background(), CreateProjectRequest{
		Name: "Bad_Name", Template: "plain", SkipStart: true,
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
