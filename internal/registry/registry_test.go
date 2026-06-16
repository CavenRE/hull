package registry

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCleanVersions(t *testing.T) {
	tags := []string{
		"latest", "16", "16.1", "16.2", "16-alpine", "16.1-bookworm",
		"15", "15.3", "14", "13", "12", "lts", "beta", "16rc1",
	}
	got := CleanVersions(tags, 6)
	want := []string{"lts", "16", "15", "14", "13", "12"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("CleanVersions = %v, want %v", got, want)
	}
}

func TestCleanVersionsPrefersBareMajor(t *testing.T) {
	got := CleanVersions([]string{"16.3", "16.2", "16"}, 6)
	if len(got) != 1 || got[0] != "16" {
		t.Errorf("expected bare 16 preferred, got %v", got)
	}
}

func TestCleanVersionsLimit(t *testing.T) {
	got := CleanVersions([]string{"9", "8", "7", "6", "5", "4", "3"}, 3)
	if strings.Join(got, ",") != "9,8,7" {
		t.Errorf("limit not applied: %v", got)
	}
}

func TestMinorVersions(t *testing.T) {
	tags := []string{
		"8.4.1", "8.4", "8.4-cli", "8.3.15", "8.3", "8.2", "8.2.27",
		"8.1", "8.0", "7.4", "latest", "rc", "8.4-fpm-bookworm",
	}
	got := MinorVersions(tags, 5)
	want := []string{"8.4", "8.3", "8.2", "8.1", "8.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("MinorVersions = %v, want %v", got, want)
	}
}

func TestFilterTags(t *testing.T) {
	tags := []string{"8.0.35", "8.0.35-debian", "8.4", "5.7.44", "5.7", "latest", "8.0.36", "bookworm"}
	got := FilterTags(tags, "8.0", 10)
	if strings.Join(got, ",") != "8.0.36,8.0.35,8.0.35-debian" {
		t.Errorf("FilterTags(8.0) = %v", got)
	}
	// Empty query returns all, versioned-newest first, non-versioned last.
	all := FilterTags(tags, "", 100)
	if all[len(all)-1] != "bookworm" && all[len(all)-1] != "latest" {
		t.Errorf("non-versioned tag should sort last, got %v", all)
	}
}

func TestTagsParsesAndCaches(t *testing.T) {
	calls := 0
	c := New()
	c.Now = time.Now
	c.Fetch = func(ctx context.Context, url string) ([]byte, error) {
		calls++
		if !strings.Contains(url, "library/postgres") {
			t.Errorf("official image should use library/: %s", url)
		}
		return []byte(`{"next":"","results":[{"name":"16"},{"name":"15"}]}`), nil
	}
	tags, err := c.Tags(context.Background(), "postgres")
	if err != nil || strings.Join(tags, ",") != "16,15" {
		t.Fatalf("tags = %v, err = %v", tags, err)
	}
	// second call is cached
	_, _ = c.Tags(context.Background(), "postgres")
	if calls != 1 {
		t.Errorf("expected 1 fetch (cached), got %d", calls)
	}
}

func TestSearch(t *testing.T) {
	c := New()
	c.Fetch = func(ctx context.Context, url string) ([]byte, error) {
		return []byte(`{"results":[{"repo_name":"node","short_description":"JS runtime","is_official":true,"star_count":1000}]}`), nil
	}
	repos, err := c.Search(context.Background(), "node")
	if err != nil || len(repos) != 1 || repos[0].Name != "node" || !repos[0].Official {
		t.Fatalf("search = %+v, err = %v", repos, err)
	}
}
