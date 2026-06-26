package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/CavenRE/hull/internal/groups"
	"github.com/CavenRE/hull/internal/jobs"
)

// Client talks to a running hulld.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// Connect returns a client for the daemon recorded in hullHome, verifying
// it actually responds. ok=false means no (live) daemon.
func Connect(hullHome string) (*Client, bool) {
	info, err := ReadDaemonFile(hullHome)
	if err != nil {
		return nil, false
	}
	c := &Client{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", info.Port),
		Token:   info.Token,
		HTTP:    &http.Client{Timeout: 0},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if _, err := c.Status(ctx); err != nil {
		return nil, false
	}
	return c, true
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		var e ErrorBody
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return fmt.Errorf("daemon: %s", e.Error)
		}
		return fmt.Errorf("daemon: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Status fetches daemon status.
func (c *Client) Status(ctx context.Context) (*StatusInfo, error) {
	var s StatusInfo
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Projects lists projects with running state.
func (c *Client) Projects(ctx context.Context) ([]ProjectInfo, error) {
	var infos []ProjectInfo
	if err := c.do(ctx, http.MethodGet, "/v1/projects", nil, &infos); err != nil {
		return nil, err
	}
	return infos, nil
}

// ProjectAction starts/stops/restarts a project by name.
func (c *Client) ProjectAction(ctx context.Context, name, action string) error {
	return c.do(ctx, http.MethodPost, "/v1/projects/"+name+"/"+action, nil, nil)
}

// CreateProject starts a creation job.
func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (jobs.Info, error) {
	var ref JobRef
	if err := c.do(ctx, http.MethodPost, "/v1/projects", req, &ref); err != nil {
		return jobs.Info{}, err
	}
	return ref.Job, nil
}

// Job fetches one job snapshot.
func (c *Client) Job(ctx context.Context, id string) (jobs.Info, error) {
	var info jobs.Info
	if err := c.do(ctx, http.MethodGet, "/v1/jobs/"+id, nil, &info); err != nil {
		return jobs.Info{}, err
	}
	return info, nil
}

// WaitJob polls a job until completion, writing new lines via print.
func (c *Client) WaitJob(ctx context.Context, id string, print func(string)) (jobs.Info, error) {
	offset := 0
	for {
		info, err := c.Job(ctx, id)
		if err != nil {
			return jobs.Info{}, err
		}
		for _, line := range info.Lines[min(offset, len(info.Lines)):] {
			print(line)
		}
		offset = len(info.Lines)
		if info.Status != jobs.StatusRunning {
			return info, nil
		}
		select {
		case <-ctx.Done():
			return info, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Config fetches the daemon's configuration view.
func (c *Client) Config(ctx context.Context) (*ConfigInfo, error) {
	var info ConfigInfo
	if err := c.do(ctx, http.MethodGet, "/v1/config", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// PutConfig replaces the daemon configuration, returning the updated view
// (including any restart_required fields).
func (c *Client) PutConfig(ctx context.Context, req ConfigInfo) (*ConfigInfo, error) {
	var info ConfigInfo
	if err := c.do(ctx, http.MethodPut, "/v1/config", req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// PatchProject updates a managed project's fields (php/domain/serve).
func (c *Client) PatchProject(ctx context.Context, name string, req PatchProjectRequest) error {
	return c.do(ctx, http.MethodPatch, "/v1/projects/"+name, req, nil)
}

// Groups fetches the virtual-group store.
func (c *Client) Groups(ctx context.Context) (*groups.Store, error) {
	var s groups.Store
	if err := c.do(ctx, http.MethodGet, "/v1/groups", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// PutGroups saves the virtual-group store.
func (c *Client) PutGroups(ctx context.Context, s *groups.Store) error {
	return c.do(ctx, http.MethodPut, "/v1/groups", s, nil)
}

// SetProjectGroup assigns a project to a group ("" to ungroup).
func (c *Client) SetProjectGroup(ctx context.Context, name, group string) error {
	return c.do(ctx, http.MethodPost, "/v1/projects/"+name+"/group", map[string]string{"group": group}, nil)
}

// AdoptCluster adopts an existing compose project as a Hull cluster.
func (c *Client) AdoptCluster(ctx context.Context, req AdoptClusterRequest) (string, error) {
	var out struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/clusters", req, &out); err != nil {
		return "", err
	}
	return out.Name, nil
}

// Dependencies fetches dependency status (Docker + embedded components).
func (c *Client) Dependencies(ctx context.Context) ([]DependencyInfo, error) {
	var deps []DependencyInfo
	if err := c.do(ctx, http.MethodGet, "/v1/dependencies", nil, &deps); err != nil {
		return nil, err
	}
	return deps, nil
}

// StopAll brings down every project and shared service Hull started, returning
// how many were stopped. It does not stop the daemon (see Shutdown).
func (c *Client) StopAll(ctx context.Context) (int, error) {
	var out struct {
		Stopped int `json:"stopped"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/stop-all", nil, &out); err != nil {
		return 0, err
	}
	return out.Stopped, nil
}

// Shutdown asks the daemon to exit.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/shutdown", nil, nil)
}
