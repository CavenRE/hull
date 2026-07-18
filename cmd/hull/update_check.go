package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/version"
)

// Update notification. Hull asks GitHub for the newest release at most once a
// day, caches the answer, and offers the update on the next interactive
// command. The check is cached rather than live so the common case costs
// nothing: only one command a day does any network work at all.
const (
	updateCheckInterval = 24 * time.Hour
	updateCheckTimeout  = 2 * time.Second
	latestReleaseAPI    = "https://api.github.com/repos/CavenRE/hull/releases/latest"
	updateStateFile     = "update-check.json"
)

// updateState is what we remember between runs, in <hullHome>/update-check.json.
type updateState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`             // newest release tag seen
	Declined  string    `json:"declined,omitempty"` // tag the user said no to
}

func init() {
	// Cobra runs only the closest PersistentPreRun in the chain; no subcommand
	// defines one, so this covers every command.
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		maybeOfferUpdate(cmd)
	}
}

// maybeOfferUpdate tells you a newer Hull exists and offers to install it. It
// is deliberately quiet: never off a terminal, never in scripted or JSON runs,
// never more than once a day, and never again for a version you declined.
// Every failure path is silent, because a broken update check must not get in
// the way of the command you actually ran.
func maybeOfferUpdate(cmd *cobra.Command) {
	if !isInteractive() || flagYes || flagJSON {
		return
	}
	if os.Getenv("HULL_NO_UPDATE_CHECK") != "" {
		return
	}
	if skipUpdateCheck(cmd) {
		return
	}

	home := flagHome
	if home == "" {
		home = config.HomeDir()
	}
	st := loadUpdateState(home)

	if time.Since(st.CheckedAt) > updateCheckInterval {
		latest, err := fetchLatestRelease(cmd.Context())
		// Stamp the time either way, so an offline machine does not re-probe on
		// every single command.
		st.CheckedAt = time.Now()
		if err == nil {
			st.Latest = latest
		}
		saveUpdateState(home, st)
	}

	if st.Latest == "" || st.Latest == st.Declined || !semverNewer(st.Latest, version.Version) {
		return
	}

	fmt.Printf("\nHull %s is available (you are on %s).\n", st.Latest, version.Version)
	ok, err := confirm("Update now?")
	if err != nil {
		return // prompt failed or was interrupted: say nothing, run the command
	}
	if !ok {
		st.Declined = st.Latest
		saveUpdateState(home, st)
		fmt.Println("Staying on the current version. Update any time with: hull update")
		return
	}

	if err := runUpdate(cmd.Context(), updateOpts{branch: "master"}); err != nil {
		fmt.Println("Update failed:", err)
		fmt.Println("Continuing with the current version.")
		return
	}
	// The binary on disk (and possibly the daemon) has just been replaced, so
	// carrying on with the old in-memory build would be a lie. Stop cleanly.
	fmt.Println("\nUpdated. Re-run your command to use the new version.")
	os.Exit(0)
}

// skipUpdateCheck suppresses the offer for commands where it would recurse, hang,
// or corrupt machine-readable output.
func skipUpdateCheck(cmd *cobra.Command) bool {
	path := cmd.CommandPath()
	for _, quiet := range []string{
		"hull update",     // would recurse
		"hull daemon run", // long-lived background process
		"hull completion", // shell script output
		"hull install",
		"hull uninstall",
		"hull help",
	} {
		if strings.HasPrefix(path, quiet) {
			return true
		}
	}
	return false
}

func updateStatePath(home string) string { return filepath.Join(home, updateStateFile) }

func loadUpdateState(home string) updateState {
	var st updateState
	data, err := os.ReadFile(updateStatePath(home))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func saveUpdateState(home string, st updateState) {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(updateStatePath(home), data, 0o644)
}

// fetchLatestRelease returns the newest published release tag (e.g. "v0.15.1").
// Short timeout: this runs in front of a command the user is waiting on.
func fetchLatestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "hull-update-check")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github returned %s", resp.Status)
	}
	var out struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	return out.TagName, nil
}

// semverRE captures the leading vMAJOR.MINOR.PATCH, ignoring any git-describe
// suffix ("v0.15.1-3-gabc1234").
var semverRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

func parseSemver(s string) ([3]int, bool) {
	var out [3]int
	m := semverRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return out, false
	}
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out, true
}

// semverNewer reports whether latest is a strictly higher release than current.
// An unparseable current version ("dev") never prompts, so local builds are not
// nagged; a dev build ahead of the newest tag compares equal and stays quiet.
func semverNewer(latest, current string) bool {
	l, ok1 := parseSemver(latest)
	c, ok2 := parseSemver(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}
