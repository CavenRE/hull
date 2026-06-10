package api

import "github.com/CavenRE/hull/internal/jobs"

// StatusInfo answers GET /v1/status.
type StatusInfo struct {
	Version  string   `json:"version"`
	TLD      string   `json:"tld"`
	Roots    []string `json:"roots"`
	HullHome string   `json:"hull_home"`
}

// ProjectInfo answers GET /v1/projects.
type ProjectInfo struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Kind    string `json:"kind"` // template name, "app", or "legacy"
	URL     string `json:"url,omitempty"`
	Running bool   `json:"running"`
	Legacy  bool   `json:"legacy,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CreateProjectRequest answers POST /v1/projects (a job).
type CreateProjectRequest struct {
	Name      string `json:"name"`
	Template  string `json:"template"`
	DB        string `json:"db,omitempty"`
	DBVersion string `json:"db_version,omitempty"`
	Redis     bool   `json:"redis,omitempty"`
	PHP       string `json:"php,omitempty"`
	Version   string `json:"version,omitempty"`
	SkipStart bool   `json:"skip_start,omitempty"`
}

// JobRef points a client at a started job.
type JobRef struct {
	Job jobs.Info `json:"job"`
}

// Event is one message on GET /v1/events.
type Event struct {
	// Type is "projects" (the running set changed).
	Type string `json:"type"`
	// Running lists compose projects currently running.
	Running []string `json:"running"`
}

// ErrorBody is the JSON error envelope.
type ErrorBody struct {
	Error string `json:"error"`
}
