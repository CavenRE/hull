package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/version"
)

const updateRepo = "https://github.com/CavenRE/hull.git"

type updateOpts struct {
	branch     string
	check      bool
	force      bool
	fromSource bool
}

func init() {
	o := updateOpts{}
	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"upgrade", "self-update"},
		Short:   "Update Hull (the CLI + daemon) to the latest version",
		Long: "Update the Hull CLI and daemon in place.\n\n" +
			"By default this downloads the prebuilt binary for your platform from the\n" +
			"latest GitHub release and swaps it in place, so it needs neither Go nor\n" +
			"git. If there is no prebuilt binary for your platform (or you pass\n" +
			"--from-source), it clones the repo and runs `go build` instead, which does\n" +
			"need Go and git. Either way it writes the fresh `hull` next to the running\n" +
			"binary, so it updates the same install you already have.\n\n" +
			"How it decides: the binary path compares the latest release tag to this\n" +
			"build's version; the source path compares the latest commit on the branch\n" +
			"(default master). If already current it does nothing unless you pass\n" +
			"--reinstall. Use --check to see whether an update is available without\n" +
			"installing it.\n\n" +
			"The daemon keeps serving the old version until you restart it (hull stop,\n" +
			"then start it again). On Windows the running image is renamed aside so the\n" +
			"swap works even while the daemon holds hull.exe.\n\n" +
			"If Hull was installed from a package manager (pacman, dpkg), this defers to\n" +
			"it instead of overwriting managed files.",
		Example: "  hull update                # download + install the latest\n" +
			"  hull update --check        # only report whether an update is available\n" +
			"  hull update --from-source  # build from source instead of downloading\n" +
			"  hull update --reinstall    # reinstall even if already up to date",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), o)
		},
	}
	cmd.Flags().StringVar(&o.branch, "branch", "master", "branch to build from (source builds only)")
	cmd.Flags().BoolVar(&o.check, "check", false, "only check whether an update is available; don't install")
	cmd.Flags().BoolVar(&o.force, "reinstall", false, "reinstall even if already up to date")
	cmd.Flags().BoolVar(&o.fromSource, "from-source", false, "build from source instead of downloading the prebuilt binary (needs Go and git)")
	cmd.Flags().BoolVarP(&o.force, "force", "f", false, "deprecated alias for --reinstall")
	_ = cmd.Flags().MarkHidden("force")
	rootCmd.AddCommand(cmd)
}

func runUpdate(ctx context.Context, o updateOpts) error {
	if !validBranchName(o.branch) {
		return fmt.Errorf("invalid branch name %q", o.branch)
	}

	// Locate the running binary; that directory is the install target.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the hull binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	// Clear any .old/.new left by a previous run (a running exe cannot delete
	// its own replaced file on Windows, so it is left for next time).
	cleanupLeftovers(self)

	// Package-manager installs are owned by the manager; don't fight it.
	if pm := packageManagerOwner(self); pm != "" {
		return fmt.Errorf("hull was installed via %s; update it there instead (%s)", pm, pmUpdateHint(pm))
	}

	// Default: download the prebuilt binary for this platform (no Go or git
	// required). Falls back to a source build when there is no asset for this
	// platform, or when --from-source is set.
	if !o.fromSource {
		handled, err := binaryUpdate(ctx, o, self)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		if !o.check {
			fmt.Println("No prebuilt binary is published for this platform; building from source instead (needs Go and git).")
		}
	}
	return sourceUpdate(ctx, o, self)
}

// binaryUpdate downloads and installs the prebuilt release binary for this
// platform. It returns handled=false (with no error) when there is no asset for
// this OS/arch, so the caller can fall back to a source build. Needs neither Go
// nor git.
func binaryUpdate(ctx context.Context, o updateOpts, self string) (bool, error) {
	asset, ok := releaseAssetName(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return false, nil // unusual platform: let the source build handle it
	}
	tag, assets, err := fetchLatestReleaseAssets(ctx)
	if err != nil {
		return true, fmt.Errorf("could not reach the GitHub releases API: %w", err)
	}
	url, ok := assets[asset]
	if !ok {
		return false, nil // this release did not publish our platform's binary
	}

	cur := version.Version
	upToDate := isSemver(cur) && !semverNewer(tag, cur)
	if o.check {
		if upToDate {
			fmt.Printf("Hull is up to date (%s).\n", version.String())
		} else {
			fmt.Printf("Update available: %s -> %s.\nRun `hull update` to install it.\n", cur, tag)
		}
		return true, nil
	}
	if upToDate && !o.force {
		fmt.Printf("Already up to date (%s). Use --reinstall to reinstall anyway.\n", version.String())
		return true, nil
	}

	tmp, err := os.MkdirTemp("", "hull-update-")
	if err != nil {
		return true, err
	}
	defer os.RemoveAll(tmp)

	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	dl := filepath.Join(tmp, "hull"+suffix)
	fmt.Printf("Downloading Hull %s (%s)...\n", tag, asset)
	if err := downloadFile(ctx, url, dl); err != nil {
		return true, fmt.Errorf("download failed: %w", err)
	}

	_, daemonRunning := daemonState()
	staged, err := stageReplacement(self, dl)
	if err != nil {
		return true, fmt.Errorf("staging hull: %w", err)
	}
	if err := commitReplacement(self, staged); err != nil {
		return true, fmt.Errorf("could not replace hull: %w", err)
	}

	fmt.Printf("Updated Hull: %s -> %s\n", version.String(), tag)
	if daemonRunning {
		printDaemonRestartHint()
	}
	return true, nil
}

// sourceUpdate is the clone-and-build path, used with --from-source or when no
// prebuilt binary exists for this platform. Needs git and Go.
func sourceUpdate(ctx context.Context, o updateOpts, self string) error {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required to build Hull from source")
	}

	latest, err := remoteHead(ctx, o.branch)
	if err != nil {
		return fmt.Errorf("could not reach the Hull repo: %w", err)
	}
	upToDate := sameCommit(version.Commit, latest)

	if o.check {
		if upToDate {
			fmt.Printf("Hull is up to date (%s).\n", version.String())
		} else {
			fmt.Printf("Update available: %s -> %s (branch %s).\nRun `hull update` to install it.\n",
				shortSHA(version.Commit), shortSHA(latest), o.branch)
		}
		return nil
	}
	if upToDate && !o.force {
		fmt.Printf("Already up to date (%s). Use --reinstall to rebuild anyway.\n", version.String())
		return nil
	}
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("Go is required to build Hull from source (https://go.dev/dl/); install it and re-run, or download a prebuilt binary from the releases page")
	}

	// Clone + build into a temp dir.
	tmp, err := os.MkdirTemp("", "hull-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "src")

	fmt.Printf("Fetching Hull (%s)...\n", o.branch)
	if err := runIO(ctx, "", "git", "clone", "--depth", "1", "--branch", o.branch, updateRepo, src); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	ver := describeVersion(ctx, src)
	ldflags := fmt.Sprintf(
		"-s -w -X github.com/CavenRE/hull/internal/version.Version=%s -X github.com/CavenRE/hull/internal/version.Commit=%s",
		ver, shortSHA(latest))

	newHull := filepath.Join(tmp, "hull"+suffix)
	fmt.Println("Building hull...")
	if err := runIO(ctx, src, "go", "build", "-ldflags", ldflags, "-o", newHull, "./cmd/hull"); err != nil {
		return fmt.Errorf("building hull failed: %w", err)
	}

	_, daemonRunning := daemonState()

	// Swap the running hull binary. On Windows a running daemon (also hull.exe)
	// locks the file; commitReplacement renames the running image aside, which
	// Windows permits, so the swap works without stopping the daemon. The old
	// daemon keeps serving until restarted onto the new binary.
	staged, err := stageReplacement(self, newHull)
	if err != nil {
		return fmt.Errorf("staging hull: %w", err)
	}
	if err := commitReplacement(self, staged); err != nil {
		return fmt.Errorf("could not replace hull: %w", err)
	}

	fmt.Printf("Updated Hull: %s -> %s\n", version.String(), ver)
	if daemonRunning {
		printDaemonRestartHint()
	}
	return nil
}

// daemonState reports the Hull home and whether a daemon is currently running,
// for post-update messaging. Best-effort: a failure to load config means "no
// daemon known".
func daemonState() (string, bool) {
	a, err := loadApp()
	if err != nil {
		return "", false
	}
	_, running := api.Connect(a.Config.HullHome)
	return a.Config.HullHome, running
}

func printDaemonRestartHint() {
	fmt.Println("A daemon is still running the previous version. Restart it to pick up the update:")
	fmt.Println("  hull stop   (then start it again, or restart your systemd --user service)")
}

// releaseAssetName maps a Go OS/arch to the release asset filename Hull
// publishes, or ok=false when no prebuilt binary is published for it.
func releaseAssetName(goos, goarch string) (string, bool) {
	switch goos {
	case "windows":
		if goarch == "amd64" {
			return "hull-windows-x64.exe", true
		}
	case "linux":
		if goarch == "amd64" {
			return "hull-linux-x64", true
		}
	case "darwin":
		switch goarch {
		case "arm64":
			return "hull-macos-apple-silicon", true
		case "amd64":
			return "hull-macos-intel", true
		}
	}
	return "", false
}

// fetchLatestReleaseAssets returns the latest release tag and a map of asset
// name -> download URL from the GitHub releases API. No git needed.
func fetchLatestReleaseAssets(ctx context.Context) (string, map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "hull-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("github returned %s", resp.Status)
	}
	var out struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", nil, err
	}
	m := make(map[string]string, len(out.Assets))
	for _, a := range out.Assets {
		m[a.Name] = a.URL
	}
	return out.TagName, m, nil
}

// downloadFile streams url to dst, following redirects (GitHub asset URLs
// redirect to a CDN). It rejects a suspiciously small result so a saved error
// page is never mistaken for a binary.
func downloadFile(ctx context.Context, url, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "hull-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github returned %s", resp.Status)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, resp.Body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return err
	}
	if n < 1<<20 { // < 1 MiB: not a real hull binary
		_ = os.Remove(dst)
		return fmt.Errorf("downloaded file is too small (%d bytes); the release asset may be missing", n)
	}
	return nil
}

// isSemver reports whether s parses as a vMAJOR.MINOR.PATCH version.
func isSemver(s string) bool {
	_, ok := parseSemver(s)
	return ok
}

// stageReplacement copies src to target+".new" in target's own directory (same
// volume, so the later rename is atomic) and returns the staged path.
func stageReplacement(target, src string) (string, error) {
	staged := target + ".new"
	if err := copyFile(src, staged, 0o755); err != nil {
		return "", err
	}
	return staged, nil
}

// commitReplacement renames staged over target. If target is in use (Windows
// locks a running exe), it moves the current file aside first, which Windows
// permits. On a failed final rename it restores the moved-aside file, and if
// that restore also fails it says so loudly rather than leaving no binary.
func commitReplacement(target, staged string) error {
	if err := os.Rename(staged, target); err == nil {
		return nil
	}
	old := target + ".old"
	_ = os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("could not move the current file aside: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		if rb := os.Rename(old, target); rb != nil {
			return fmt.Errorf("replace failed and rollback failed: the previous binary is at %q, move it back to %q manually (replace error: %v; rollback error: %v)", old, target, err, rb)
		}
		return err
	}
	_ = os.Remove(old) // best effort; may be locked on Windows until we exit
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// OpenFile's mode is subject to umask on Unix; re-assert the exec bit there.
	// Skip on Windows, where Chmod on an .exe can fail spuriously.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dst, mode)
	}
	return nil
}

// cleanupLeftovers best-effort removes .old/.new siblings from a prior run.
func cleanupLeftovers(targets ...string) {
	for _, t := range targets {
		_ = os.Remove(t + ".old")
		_ = os.Remove(t + ".new")
	}
}

// remoteHead returns the latest commit SHA on the given branch of the Hull repo.
func remoteHead(ctx context.Context, branch string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "ls-remote", updateRepo, "refs/heads/"+branch).Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("branch %q not found on the remote", branch)
	}
	return fields[0], nil
}

// describeVersion returns a human version string for the cloned source. A shallow
// clone has no tags, so this is usually the short SHA; that is fine for a
// source build.
func describeVersion(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return "source"
	}
	if v := strings.TrimSpace(string(out)); v != "" {
		return v
	}
	return "source"
}

// stopDaemonAndWait asks the daemon to shut down and waits until it is gone.
func stopDaemonAndWait(ctx context.Context, home string) {
	if client, ok := api.Connect(home); ok {
		_ = client.Shutdown(ctx)
	}
	for i := 0; i < 50; i++ { // up to ~5s
		if _, ok := api.Connect(home); !ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// sameCommit reports whether two commit identifiers refer to the same commit,
// tolerating one being an abbreviated SHA of the other. Non-SHA build stamps
// ("dev", "none", "aur", or anything under 7 chars) are treated as "not the
// same", so those builds are always offered an update.
func sameCommit(local, remote string) bool {
	n := len(local)
	if len(remote) < n {
		n = len(remote)
	}
	if n < 7 {
		return false
	}
	return strings.EqualFold(local[:n], remote[:n])
}

// packageManagerOwner reports the package manager that owns the given path, or
// "" if it is not a managed file (or we cannot tell). Linux only.
func packageManagerOwner(path string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, err := exec.LookPath("pacman"); err == nil {
		if exec.Command("pacman", "-Qo", path).Run() == nil {
			return "pacman"
		}
	}
	if _, err := exec.LookPath("dpkg"); err == nil {
		if exec.Command("dpkg", "-S", path).Run() == nil {
			return "dpkg"
		}
	}
	return ""
}

func pmUpdateHint(pm string) string {
	switch pm {
	case "pacman":
		return "e.g. yay -Syu hull, or sudo pacman -Syu"
	case "dpkg":
		return "update via your apt source, or reinstall the .deb"
	}
	return "use your package manager"
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// validBranchName allows the characters git branch names use and rejects
// anything that could be read as a flag or shell metacharacter. Args go
// straight to exec (no shell), so this is defence in depth, not the only guard.
func validBranchName(b string) bool {
	if b == "" || strings.HasPrefix(b, "-") {
		return false
	}
	for _, r := range b {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '/':
		default:
			return false
		}
	}
	return true
}

// runIO runs a command with stdio attached (so the user sees progress/errors).
func runIO(ctx context.Context, dir, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
