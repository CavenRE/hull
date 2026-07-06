package main

import (
	"context"
	"fmt"
	"io"
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
	branch string
	check  bool
	force  bool
}

func init() {
	o := updateOpts{}
	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"upgrade", "self-update"},
		Short:   "Update Hull (the CLI + daemon) to the latest version",
		Long: "Update the Hull CLI and daemon in place by rebuilding them from source.\n\n" +
			"There are no prebuilt CLI releases yet, so this clones the Hull repo and\n" +
			"runs `go build`, exactly the way you installed it. It needs git and Go on\n" +
			"your PATH. It writes the fresh `hull` and `hulld` next to the running\n" +
			"binary (whatever directory `hull` itself lives in), so it updates the same\n" +
			"install you already have.\n\n" +
			"How it decides: it reads the latest commit on the branch (default master)\n" +
			"with `git ls-remote` and compares it to the version this binary was built\n" +
			"from. If they match it does nothing unless you pass --force. Use --check to\n" +
			"see whether an update is available without installing it.\n\n" +
			"The daemon: your running daemon keeps serving until you restart it, so the\n" +
			"new version takes effect on the next `hulld` start. On Windows the daemon\n" +
			"holds a lock on hulld.exe, so if replacing it needs the file free Hull stops\n" +
			"the daemon first, then tells you to start it again.\n\n" +
			"If Hull was installed from a package manager (pacman, dpkg), this defers to\n" +
			"it instead of overwriting managed files.",
		Example: "  hull update            # rebuild + install the latest\n" +
			"  hull update --check    # only report whether an update is available\n" +
			"  hull update --branch master\n" +
			"  hull update --force    # reinstall even if already up to date",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), o)
		},
	}
	cmd.Flags().StringVar(&o.branch, "branch", "master", "branch to build from")
	cmd.Flags().BoolVar(&o.check, "check", false, "only check whether an update is available; don't install")
	cmd.Flags().BoolVarP(&o.force, "force", "f", false, "reinstall even if already up to date")
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
	binDir := filepath.Dir(self)
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	hulldTarget := filepath.Join(binDir, "hulld"+suffix)

	// Clear any .old/.new left by a previous run (a running exe cannot delete
	// its own replaced file on Windows, so it is left for next time).
	cleanupLeftovers(self, hulldTarget)

	// Package-manager installs are owned by the manager; don't fight it.
	if pm := packageManagerOwner(self); pm != "" {
		return fmt.Errorf("hull was installed via %s; update it there instead (%s)", pm, pmUpdateHint(pm))
	}

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required to check for and fetch updates")
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
		fmt.Printf("Already up to date (%s). Use --force to rebuild anyway.\n", version.String())
		return nil
	}

	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("Go is required to build Hull from source (https://go.dev/dl/); install it and re-run")
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
	newHulld := filepath.Join(tmp, "hulld"+suffix)
	fmt.Println("Building hull + hulld...")
	if err := runIO(ctx, src, "go", "build", "-ldflags", ldflags, "-o", newHull, "./cmd/hull"); err != nil {
		return fmt.Errorf("building hull failed: %w", err)
	}
	if err := runIO(ctx, src, "go", "build", "-ldflags", ldflags, "-o", newHulld, "./cmd/hulld"); err != nil {
		return fmt.Errorf("building hulld failed: %w", err)
	}

	// Where a daemon would be, for stopping/messaging.
	home := ""
	if a, err := loadApp(); err == nil {
		home = a.Config.HullHome
	}
	daemonRunning := false
	if home != "" {
		_, daemonRunning = api.Connect(home)
	}

	// Stage both new binaries (as *.new in the install dir) before swapping
	// either, so a copy failure leaves the install untouched. The two swaps
	// that follow are quick renames, keeping the version-skew window small.
	stagedHulld, err := stageReplacement(hulldTarget, newHulld)
	if err != nil {
		return fmt.Errorf("staging hulld: %w", err)
	}
	stagedHull, err := stageReplacement(self, newHull)
	if err != nil {
		_ = os.Remove(stagedHulld)
		return fmt.Errorf("staging hull: %w", err)
	}

	// Swap hulld. If the file is locked (Windows, daemon running), stop the
	// daemon and retry. The daemon is re-checked here, not at an earlier probe,
	// so a daemon that started mid-update is still handled.
	daemonStopped := false
	if err := commitReplacement(hulldTarget, stagedHulld); err != nil {
		if home != "" {
			if _, ok := api.Connect(home); ok {
				fmt.Println("Stopping the daemon to replace it...")
				stopDaemonAndWait(ctx, home)
				daemonStopped = true
			}
		}
		if err2 := commitReplacement(hulldTarget, stagedHulld); err2 != nil {
			_ = os.Remove(stagedHull) // nothing swapped yet: leave both binaries as they were
			return fmt.Errorf("could not replace hulld: %w", err2)
		}
	}

	// Swap the running hull binary itself.
	if err := commitReplacement(self, stagedHull); err != nil {
		return fmt.Errorf("hulld was updated but replacing hull failed; re-run `hull update` to finish: %w", err)
	}

	fmt.Printf("Updated Hull: %s -> %s\n", version.String(), ver)

	switch {
	case daemonStopped:
		fmt.Println("The daemon was stopped for the update. Start it again with: hulld")
	case daemonRunning:
		fmt.Println("A daemon is still running the previous version. Restart it to pick up the update:")
		fmt.Println("  hull daemon stop && hulld   (or restart your systemd --user service)")
	}
	return nil
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
