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
	Kind    string `json:"kind"` // template name, "app", "legacy", "folder", "invalid"
	URL     string `json:"url,omitempty"`
	Running bool   `json:"running"`
	Legacy  bool   `json:"legacy,omitempty"`
	Error   string `json:"error,omitempty"`
	PHP     string `json:"php,omitempty"`
	// Served is whether the project has a routed domain (serve toggle).
	Served bool `json:"served"`
	// Group is the virtual group label this project belongs to ("" = none).
	Group string `json:"group,omitempty"`
	// Services are the manifest-declared service links.
	Services []ProjectServiceInfo `json:"services,omitempty"`
	// Routes are a cluster's served subdomains (type: cluster only).
	Routes []ClusterRouteInfo `json:"routes,omitempty"`
}

// ClusterInfo answers GET /v1/clusters , adopted/managed cluster projects,
// reconciled with the started ledger so out-of-root clusters are included.
type ClusterInfo struct {
	Name        string             `json:"name"`
	Dir         string             `json:"dir"`
	ComposeRoot string             `json:"compose_root,omitempty"`
	Running     bool               `json:"running"`
	// BaseDomain is the domain routes nest under ("" = Hull's TLD).
	BaseDomain string `json:"base_domain,omitempty"`
	// Ingress is how Hull serves the URLs: "" (none), "delegate", or "hull".
	Ingress string             `json:"ingress,omitempty"`
	Routes  []ClusterRouteInfo `json:"routes,omitempty"`
}

// ClusterRouteInfo is one subdomain→service route of a cluster.
type ClusterRouteInfo struct {
	Key       string `json:"key"`
	Subdomain string `json:"subdomain"`
	Service   string `json:"service"`
	Port      int    `json:"port"`
	Served    bool   `json:"served"`
	// Aliases are extra subdomain labels for the same service.
	Aliases []string `json:"aliases,omitempty"`
	// Hosts are the fully-qualified hostnames this route resolves to
	// (subdomain + aliases under the cluster's domain), e.g. api.tapkit.local.
	Hosts []string `json:"hosts,omitempty"`
}

// DetectInfo answers GET /v1/detect , file-based project detection.
type DetectInfo struct {
	Kind     string `json:"kind"`     // laravel|wordpress|plain|python|node|go|docker|static
	Template string `json:"template"` // PHP site template (laravel|wordpress|plain)
	PHP      string   `json:"php,omitempty"`
	DB       string   `json:"db,omitempty"`
	Database string   `json:"database,omitempty"`
	Redis    bool     `json:"redis,omitempty"`
	Extras   []string `json:"extras,omitempty"` // mailpit, meilisearch, …
	PHPKind  bool     `json:"php_kind"`         // true → importable as a PHP site
}

// AdoptClusterRequest answers POST /v1/clusters.
type AdoptClusterRequest struct {
	Dir          string   `json:"dir"`
	Name         string   `json:"name,omitempty"`
	ComposeRoot  string   `json:"compose_root,omitempty"`
	ComposeFiles []string `json:"compose_files,omitempty"`
	Profiles     []string `json:"profiles,omitempty"`
}

// CreateClusterRequest answers POST /v1/clusters/create (a job).
type CreateClusterRequest struct {
	Name        string                 `json:"name"`
	Root        string                 `json:"root,omitempty"`
	ComposeRoot string                 `json:"compose_root,omitempty"`
	Managed     bool                   `json:"managed"`
	NoStart     bool                   `json:"no_start,omitempty"`
	Containers  []ClusterContainerSpec `json:"containers"`
}

// ClusterContainerSpec is one wizard container card.
type ClusterContainerSpec struct {
	Name     string                   `json:"name"`
	Template string                   `json:"template,omitempty"`
	Image    string                   `json:"image,omitempty"`
	Version  string                   `json:"version,omitempty"`
	Port     int                      `json:"port,omitempty"`
	Serve    bool                     `json:"serve,omitempty"`
	Services []ClusterCardServiceSpec `json:"services,omitempty"`
}

// ClusterCardServiceSpec is one linked service on a container card.
type ClusterCardServiceSpec struct {
	Engine  string `json:"engine"`
	Version string `json:"version,omitempty"`
}

// ProjectServiceInfo is one service link in a project manifest.
type ProjectServiceInfo struct {
	Key      string `json:"key"` // db, redis, mail
	Engine   string `json:"engine"`
	Version  string `json:"version,omitempty"`
	Mode     string `json:"mode"`               // dedicated | shared
	Instance string `json:"instance,omitempty"` // shared instance name
}

// UnlinkRequest answers POST /v1/projects/{name}/unlink.
type UnlinkRequest struct {
	Key string `json:"key"`
}

// SetRouteRequest answers PUT /v1/clusters/{name}/routes/{key}: assign a URL
// (subdomain) to one of the cluster's services.
type SetRouteRequest struct {
	Service string   `json:"service"`
	Port    int      `json:"port"`
	Aliases []string `json:"aliases,omitempty"`
	Serve   *bool    `json:"serve,omitempty"`
}

// SetClusterConfigRequest answers PUT /v1/clusters/{name}: update base_domain
// and/or ingress. A nil field is left unchanged; an empty string clears it.
type SetClusterConfigRequest struct {
	BaseDomain *string `json:"base_domain,omitempty"`
	Ingress    *string `json:"ingress,omitempty"`
}

// SetupStepResult answers the setup endpoints.
type SetupStepResult struct {
	Done bool `json:"done"`
	// Manual carries step-by-step instructions when elevation was not
	// available (the GUI shows them instead of failing).
	Manual string `json:"manual,omitempty"`
}

// ReapplyStep is one line of a re-run-setup result.
type ReapplyStep struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | manual
	Detail string `json:"detail"`
	Manual string `json:"manual,omitempty"` // command to run when elevation is needed
}

// ReapplyResult answers POST /v1/setup/reapply: everything the daemon could
// re-apply without elevation, plus the commands for the steps that need it.
type ReapplyResult struct {
	Steps []ReapplyStep `json:"steps"`
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
	Serve     *bool  `json:"serve,omitempty"`
	SkipStart bool   `json:"skip_start,omitempty"`
}

// JobRef points a client at a started job.
type JobRef struct {
	Job jobs.Info `json:"job"`
}

// ImportRequest answers POST /v1/imports (a job): adopt an unmanaged
// folder or legacy project already inside a root.
type ImportRequest struct {
	Name string `json:"name"`
}

// ServiceInfo answers GET /v1/services.
type ServiceInfo struct {
	Name      string `json:"name"`    // postgres-16
	Engine    string `json:"engine"`  // postgres
	Version   string `json:"version"` // 16
	Container string `json:"container"`
	Running   bool   `json:"running"`
	// URL is the instance's web UI when it has one (mailpit, adminer).
	URL string `json:"url,omitempty"`
	// Connection details for desktop tools (stable loopback publishing).
	Host     string `json:"host,omitempty"`
	HostPort int    `json:"host_port,omitempty"`
	Username string `json:"username,omitempty"`
	// LinkedProjects are manifest-declared consumers of this instance.
	LinkedProjects []string `json:"linked_projects,omitempty"`
}

// ConfigInfo answers GET/PUT /v1/config.
type ConfigInfo struct {
	TLD      string   `json:"tld"`
	Roots    []string `json:"roots"`
	// Loopback is the router/DNS bind address (127.0.0.x); editable in
	// Settings › Local domain. Empty in a request leaves it unchanged.
	Loopback string `json:"loopback,omitempty"`
	Defaults struct {
		PHP    string `json:"php"`
		Editor string `json:"editor"`
		DBTool string `json:"db_tool"`
	} `json:"defaults"`
	// RestartRequired (response only): changed fields needing a daemon
	// restart to apply.
	RestartRequired []string `json:"restart_required,omitempty"`
}

// OpenRequest answers POST /v1/projects/{name}/open.
type OpenRequest struct {
	Target string `json:"target"` // folder | editor
}

// PatchProjectRequest answers PATCH /v1/projects/{name}.
type PatchProjectRequest struct {
	PHP    *string `json:"php,omitempty"`
	Domain *string `json:"domain,omitempty"`
	Serve  *bool   `json:"serve,omitempty"`
}

// AddServiceRequest answers POST /v1/services (a job , image pulls).
type AddServiceRequest struct {
	Engine  string `json:"engine"`
	Version string `json:"version,omitempty"`
}

// LinkRequest answers POST /v1/services/{name}/link (a job).
type LinkRequest struct {
	Project string `json:"project"`
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
