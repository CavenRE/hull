package engine

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/CavenRE/hull/internal/compose"
	"github.com/CavenRE/hull/internal/platform"
	"github.com/CavenRE/hull/internal/state"
	"github.com/CavenRE/hull/internal/templates"
)

// Moving a project into WSL is the one change that removes Hull's Windows
// performance problem rather than working around it.
//
// Measured on this machine, reading a 1,059 file WordPress tree from a
// container: 4,450 ms when the files sit on a Windows drive, 10 ms when they
// sit in a WSL distro, and 20 ms from the container's own disk. The distro is
// not a compromise between the two; it is at least as fast as container-native.
//
// What makes it cheap to implement is that almost nothing has to change. Docker
// Desktop serves a \\wsl.localhost\<distro>\... path through a per-distro mount
// service, and Compose resolves a relative "./" bind mount against wherever the
// compose file lives, so a project that simply sits at such a path needs no new
// rendering, no new mount syntax, and no daemon inside the distro. Hull stays on
// Windows and keeps owning DNS, the router and the ports. Only the files move.
//
// Docker also keys containers and named volumes on the compose project name,
// which Hull writes into every generated compose.yaml from the manifest, not
// from the folder. So a move keeps the project's identity, its databases, and
// its data volumes. Nothing is recreated.

// movedSuffix is appended to the source directory after a successful copy.
//
// The source has to stop looking like a project, or Hull finds two of them and
// silently picks one: a folder under a parked root counts as a project if it has
// a hull.yaml, and still counts as a legacy one if it has only a compose.yaml,
// so retiring the manifest alone is not enough. Renaming the directory settles
// it in one move. Nothing is deleted; deciding when to throw away their code is
// the user's call, not Hull's.
const movedSuffix = ".hull-moved"

// wslIdentityTimeout bounds the identity lookup a render does. Generous because
// it may have to wake a distribution that is not running, and the alternative
// to waiting is rendering a project that cannot write its own files. Paid once:
// the answer is cached for the life of the process.
const wslIdentityTimeout = 2 * time.Minute

// RelocateOptions describes a move into WSL.
type RelocateOptions struct {
	// Distro is the WSL distribution to move into.
	Distro string
	// Dest is the destination directory as a Linux path inside the distro.
	Dest string
}

// WSLDestination works out where a project should live inside a distro,
// mirroring the layout the user already has on Windows: a project in a folder
// called Sites lands in ~/Sites.
func WSLDestination(ctx context.Context, distro, projectDir, projectName string) (string, error) {
	home, err := platform.WSLHome(ctx, distro)
	if err != nil {
		return "", err
	}
	parent := filepath.Base(filepath.Dir(projectDir))
	if parent == "" || parent == "." || strings.ContainsAny(parent, `\/:`) {
		parent = "Sites"
	}
	return path.Join(home, parent, projectName), nil
}

// RelocateToWSL copies a project into a WSL distribution and returns its new
// Windows path. It stops the project first, copies, verifies, and takes the old
// copy out of Hull's sight; it does NOT start the project again or update the
// config, because those belong to the caller that owns registration.
//
// The copy is performed by the distro's own cp rather than from Windows. That
// is not a detail: the distro writes to ext4 directly, so it reads the source
// across the boundary once instead of paying the crossing on both sides.
func (e *Engine) RelocateToWSL(ctx context.Context, p *state.Project, opts RelocateOptions, logf func(string, ...any)) (newDir, retiredDir string, err error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if !platform.WSLAvailable() {
		return "", "", fmt.Errorf("WSL is not available on this machine, so there is nowhere to move to")
	}
	// A cluster wraps somebody else's compose stack, whose files can reference
	// paths outside the project directory and whose compose root is a
	// subdirectory Hull does not own. Copying the folder would move half of it.
	// Refuse rather than half-work; the speed argument still applies, but moving
	// one of those is a decision its author has to make.
	if isCluster(p) {
		return "", "", fmt.Errorf("%s is a cluster, which wraps a compose stack Hull did not generate and may reference paths outside its folder. Move it by hand, then re-import it with: hull import <new path>", p.Name)
	}
	if platform.OnWSLFilesystem(p.Dir) {
		return "", "", fmt.Errorf("%s already lives in WSL (%s)", p.Name, p.Dir)
	}
	source := platform.WSLLinuxPath(p.Dir)
	if source == "" {
		return "", "", fmt.Errorf("cannot express %s as a path WSL can read; only drive paths can be moved", p.Dir)
	}
	if !platform.WSLDockerIntegration(ctx, opts.Distro) {
		return "", "", fmt.Errorf("Docker Desktop's WSL integration is off for %s, and without it Docker cannot mount files from that distro. Turn it on in Docker Desktop (Settings > Resources > WSL Integration > %s, then Apply & Restart) and run this again", opts.Distro, opts.Distro)
	}

	// The destination is a Linux path inside the distro, and a wrong one is not
	// caught later: a relative path would be resolved against whatever the
	// distro's working directory happens to be, and "~" is a shell convention
	// that nothing here expands, so it would create a directory literally named
	// "~". Both silently put the copy somewhere the user did not ask for.
	if !strings.HasPrefix(opts.Dest, "/") {
		return "", "", fmt.Errorf("--dest must be an absolute path inside %s, for example /home/you/Sites/%s (got %q)", opts.Distro, p.Name, opts.Dest)
	}

	// Resolve the identity now, before anything moves. The render needs it to
	// make the moved project writable, and a render cannot report why it failed;
	// finding out here means failing with an explanation while the project is
	// still exactly where it was. It also warms the cache the render reads.
	uid, _, err := platform.WSLDistroUser(ctx, opts.Distro)
	if err != nil {
		return "", "", fmt.Errorf("could not work out which user %s runs as, and without that the moved project would not be writable by its own web server: %w", opts.Distro, err)
	}
	if uid == "0" {
		logf("note: %s runs as root, so the site will take ownership of the paths it writes", opts.Distro)
	}

	newDir = platform.UNCPath(opts.Distro, opts.Dest)
	if _, err := os.Stat(newDir); err == nil {
		return "", "", fmt.Errorf("%s already exists inside %s; move it aside or pass a different --dest", opts.Dest, opts.Distro)
	}

	// Stop before copying. A running WordPress or Laravel writes caches and
	// session files continuously, and copying underneath that produces a subtly
	// different tree at the destination.
	logf("stopping %s", p.Name)
	if err := e.Down(ctx, p); err != nil {
		return "", "", fmt.Errorf("stopping %s before the move: %w", p.Name, err)
	}

	// Say plainly what Hull cannot do for this template. A Windows bind mount
	// fakes ownership, so a node or python project never had to think about it;
	// a WSL mount does not, and Hull only knows how to remap the identity for
	// its PHP images. The project still runs and is still far faster there, but
	// files its container creates will belong to root.
	if p.Manifest != nil {
		if def, ok := templates.Site(p.Manifest.Template); ok && !def.IsPHP() {
			logf("note: a %s container writes files as root, and unlike a Windows drive a WSL one keeps that ownership, so anything it creates (node_modules, build output) will need sudo to delete from your editor", p.Manifest.Template)
		}
	}

	logf("copying %s into %s (this reads every file once, so it takes a moment)", p.Name, opts.Distro)
	copyErr := wslCopy(ctx, opts.Distro, source, opts.Dest)
	if copyErr == nil {
		// Verify before touching the original. The whole safety of this command
		// rests on never disturbing the source until the destination is known
		// good.
		if _, err := os.Stat(filepath.Join(newDir, "hull.yaml")); err != nil {
			copyErr = fmt.Errorf("the copy finished but %s has no hull.yaml", newDir)
		}
	}
	if copyErr != nil {
		// Clear the half-written destination. Leaving it would make the obvious
		// next step (run the command again) fail on "already exists", and the
		// user would have to work out for themselves that the leftover is
		// incomplete rather than a second copy of their project.
		// Put the project back the way it was found. Stopping it was Hull's
		// doing, so leaving it down after a failure that changed nothing else
		// would be a second, unrelated problem for the user to notice.
		if upErr := e.Up(context.WithoutCancel(ctx), p); upErr != nil {
			logf("could not start %s again after the failed move: %v", p.Name, upErr)
		}
		if rmErr := wslRemove(context.WithoutCancel(ctx), opts.Distro, opts.Dest); rmErr != nil {
			return "", "", fmt.Errorf("%w. A partial copy was left at %s and could not be cleaned up (%v); delete it before trying again. Your original is untouched at %s", copyErr, newDir, rmErr, p.Dir)
		}
		return "", "", fmt.Errorf("%w. The partial copy has been cleaned up and your project is still running from %s", copyErr, p.Dir)
	}

	// Take the original out of Hull's sight without deleting anything.
	retired, err := retireSource(p.Dir)
	if err != nil {
		return "", "", fmt.Errorf("copied to %s, but could not move the old folder aside (%w). Rename or delete %s yourself before starting the project, or Hull will find two projects named %s and silently pick one", newDir, err, p.Dir, p.Name)
	}
	logf("old folder moved aside to %s", retired)
	return newDir, retired, nil
}

// retireSource renames the moved-from directory so it no longer reads as a
// project, and returns its new name. Falls back to retiring the two files that
// make it one, for the case where the directory itself cannot be renamed
// because something on Windows still holds a handle to it.
func retireSource(dir string) (string, error) {
	retired := dir + movedSuffix
	for i := 2; ; i++ {
		if _, err := os.Stat(retired); err != nil {
			break
		}
		retired = fmt.Sprintf("%s%s.%d", dir, movedSuffix, i)
	}
	if err := os.Rename(dir, retired); err == nil {
		// Renaming the folder is not quite enough on its own: under a parked
		// root it would still be scanned, and a bare compose file is all it
		// takes to be adopted as a legacy project, complete with a domain of its
		// own. Retire the marker files so the leftover is just a folder. If that
		// fails, say so: a leftover that still reads as a project would shadow
		// the one we just moved, and reporting success would hide it.
		if err := retireMarkers(retired); err != nil {
			return "", err
		}
		return retired, nil
	}
	// A directory Windows will not rename, because an editor or a shell is
	// sitting in it, still has to stop being discoverable. The marker files are
	// the only reason it is.
	if err := retireMarkers(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// retireMarkers renames the files that make a directory a Hull project. The
// compose list has to match every spelling state.load looks for, or a leftover
// folder keeps being adopted as a legacy project under whichever one was missed.
func retireMarkers(dir string) error {
	var failed error
	for _, name := range []string{"hull.yaml", "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		src := filepath.Join(dir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, src+movedSuffix); err != nil {
			failed = err
		}
	}
	return failed
}

// wslCopy runs the copy inside the distro, preserving ownership, timestamps and
// symlinks.
func wslCopy(ctx context.Context, distro, source, dest string) error {
	script := fmt.Sprintf("set -e; mkdir -p %s; cp -a %s/. %s/", shellQuote(path.Dir(dest)), shellQuote(source), shellQuote(dest))
	if out, err := platform.WSLRun(ctx, distro, script); err != nil {
		detail := strings.TrimSpace(out)
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("copying into %s failed: %s", distro, detail)
	}
	return nil
}

// wslRemove deletes a directory inside a distro. Used only to clear a
// destination Hull created itself and then failed to fill, which is why it can
// be this blunt: the path is one Hull chose and refused to reuse if it already
// existed, so there is nothing of the user's inside it.
func wslRemove(ctx context.Context, distro, dir string) error {
	if !strings.HasPrefix(dir, "/") || dir == "/" {
		return fmt.Errorf("refusing to remove %q", dir)
	}
	if _, err := platform.WSLRun(ctx, distro, "rm -rf "+shellQuote(dir)); err != nil {
		return err
	}
	return nil
}

// shellQuote wraps a path for a POSIX shell. Project paths routinely contain
// spaces on Windows ("My Site"), and an unquoted one silently copies the wrong
// thing rather than failing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// applyWSLIdentity gives the render the host identity a project inside WSL must
// run as.
//
// On a Windows bind mount, ownership is a fiction: Docker Desktop presents every
// file as writable whoever owns it, so Hull deliberately leaves the identity
// empty and lets the container run as its image's own user. A WSL mount is a
// real Linux filesystem, where the project's files belong to the distro user and
// the container's www-data cannot write them. That is the same situation native
// Linux has always been in, and Hull already has the answer: pass the host
// identity, which switches on the image's own user-remapping for serversideup
// and APACHE_RUN_USER for the WordPress image.
//
// Failing to resolve the identity is not fatal. The project still renders and
// still serves; it just cannot write its own files, which the permission script
// reports far more clearly than a failed render would.
func applyWSLIdentity(ctx *compose.Context, dir string) {
	if ctx.HostUID != "" || !platform.OnWSLFilesystem(dir) {
		return
	}
	distro := platform.WSLDistroOf(dir)
	if distro == "" {
		return
	}
	// Its own context: this is a cached machine-level fact, not something that
	// belongs to whatever request happens to have triggered a render.
	lookup, cancel := context.WithTimeout(context.Background(), wslIdentityTimeout)
	defer cancel()
	uid, gid, err := platform.WSLDistroUser(lookup, distro)
	if err != nil {
		return
	}
	// A distro whose default user is root needs no remapping and must not get
	// one: Apache refuses to run as root, and remapping the PHP-FPM user to uid
	// 0 would run the whole web server privileged. Root-owned files are instead
	// handled the way they always have been, by the boot script chowning the
	// paths the app must write.
	if uid == "0" {
		return
	}
	ctx.HostUID, ctx.HostGID = uid, gid
}
