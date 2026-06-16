// Package registry queries Docker Hub for live image tags and repository
// search, so version pickers reflect what's actually published (latest LTS
// down to a few majors back) instead of a hardcoded, stale list. Results
// are cached; callers supply a static fallback for the offline/error case.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Repo is one Docker Hub search result.
type Repo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Official    bool   `json:"official"`
	Stars       int    `json:"stars"`
}

// Client fetches and caches Docker Hub data.
type Client struct {
	HTTP *http.Client
	TTL  time.Duration
	// Fetch is injectable for tests; nil uses HTTP.
	Fetch func(ctx context.Context, url string) ([]byte, error)
	Now   func() time.Time

	mu    sync.Mutex
	cache map[string]entry
}

type entry struct {
	data []byte
	at   time.Time
}

// New returns a client with sane defaults.
func New() *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: 8 * time.Second},
		TTL:   6 * time.Hour,
		Now:   time.Now,
		cache: map[string]entry{},
	}
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	c.mu.Lock()
	if e, ok := c.cache[url]; ok && c.Now().Sub(e.at) < c.TTL {
		c.mu.Unlock()
		return e.data, nil
	}
	c.mu.Unlock()

	var (
		data []byte
		err  error
	)
	if c.Fetch != nil {
		data, err = c.Fetch(ctx, url)
	} else {
		data, err = c.httpGet(ctx, url)
	}
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[url] = entry{data: data, at: c.Now()}
	c.mu.Unlock()
	return data, nil
}

func (c *Client) httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker hub: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// repoPath normalises a repo to the Hub API path ("postgres" →
// "library/postgres", "getmeili/meilisearch" stays).
func repoPath(repo string) string {
	if strings.Contains(repo, "/") {
		return repo
	}
	return "library/" + repo
}

// Tags returns up to ~300 recent tag names for a repository.
func (c *Client) Tags(ctx context.Context, repo string) ([]string, error) {
	var tags []string
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100&ordering=last_updated", repoPath(repo))
	for page := 0; page < 3 && url != ""; page++ {
		data, err := c.get(ctx, url)
		if err != nil {
			if page == 0 {
				return nil, err
			}
			break
		}
		var body struct {
			Next    string `json:"next"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			break
		}
		for _, r := range body.Results {
			tags = append(tags, r.Name)
		}
		url = body.Next
	}
	return tags, nil
}

// Search queries Docker Hub repositories.
func (c *Client) Search(ctx context.Context, query string) ([]Repo, error) {
	url := fmt.Sprintf("https://hub.docker.com/v2/search/repositories/?query=%s&page_size=25", urlQuery(query))
	data, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	var body struct {
		Results []struct {
			RepoName    string `json:"repo_name"`
			Description string `json:"short_description"`
			Official    bool   `json:"is_official"`
			Stars       int    `json:"star_count"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	repos := make([]Repo, 0, len(body.Results))
	for _, r := range body.Results {
		repos = append(repos, Repo{Name: r.RepoName, Description: r.Description, Official: r.Official, Stars: r.Stars})
	}
	return repos, nil
}

func urlQuery(s string) string {
	return strings.NewReplacer(" ", "+", "&", "", "?", "", "#", "").Replace(strings.TrimSpace(s))
}

var (
	cleanRE = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?$`)
	bareRE  = regexp.MustCompile(`^v?\d+$`)
)

type semver struct {
	raw string
	n   [3]int
}

// CleanVersions reduces a tag list to one representative per major version
// (preferring the bare "16" over "16.3"), newest first, plus an "lts" entry
// when present — the "latest LTS down to N back" shape the picker wants.
func CleanVersions(tags []string, limit int) []string {
	byMajor := map[int][]semver{}
	hasLTS := false
	for _, t := range tags {
		if strings.EqualFold(t, "lts") {
			hasLTS = true
			continue
		}
		m := cleanRE.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		var v semver
		v.raw = t
		v.n[0], _ = strconv.Atoi(m[1])
		if m[2] != "" {
			v.n[1], _ = strconv.Atoi(m[2])
		}
		if m[3] != "" {
			v.n[2], _ = strconv.Atoi(m[3])
		}
		byMajor[v.n[0]] = append(byMajor[v.n[0]], v)
	}

	majors := make([]int, 0, len(byMajor))
	for maj := range byMajor {
		majors = append(majors, maj)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))

	var out []string
	if hasLTS {
		out = append(out, "lts")
	}
	for _, maj := range majors {
		group := byMajor[maj]
		sort.Slice(group, func(i, j int) bool { return less(group[j].n, group[i].n) })
		rep := group[0].raw
		for _, g := range group {
			if bareRE.MatchString(g.raw) {
				rep = g.raw
				break
			}
		}
		out = append(out, rep)
		if len(out) >= limit {
			break
		}
	}
	return out
}

var verPrefixRE = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

// FilterTags returns tags containing q (case-insensitive), newest-version
// first, capped at limit — the "search for a specific version" path. Tags
// with no numeric prefix sort last.
func FilterTags(tags []string, q string, limit int) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		if seen[t] {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(t), q) {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aok := verPrefix(out[i])
		bj, bok := verPrefix(out[j])
		if aok != bok {
			return aok // versioned tags first
		}
		if aok && bok && ai != bj {
			return less(bj, ai) // newest first
		}
		return out[i] < out[j]
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func verPrefix(tag string) ([3]int, bool) {
	m := verPrefixRE.FindStringSubmatch(tag)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	v[0], _ = strconv.Atoi(m[1])
	if m[2] != "" {
		v[1], _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		v[2], _ = strconv.Atoi(m[3])
	}
	return v, true
}

func less(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

var minorRE = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.\d+)?$`)

// MinorVersions reduces tags to distinct X.Y versions (e.g. PHP 8.4, 8.3),
// newest first — used where the minor matters, not just the major.
func MinorVersions(tags []string, limit int) []string {
	seen := map[string]bool{}
	var vers []semver
	for _, t := range tags {
		m := minorRE.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		key := m[1] + "." + m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		vers = append(vers, semver{raw: key, n: [3]int{maj, min}})
	}
	sort.Slice(vers, func(i, j int) bool { return less(vers[j].n, vers[i].n) })
	out := make([]string, 0, limit)
	for _, v := range vers {
		out = append(out, v.raw)
		if len(out) >= limit {
			break
		}
	}
	return out
}
