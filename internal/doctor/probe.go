package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// This is the difference between telling someone their filesystem is slow and
// showing them. "Your mount costs 3.8 ms per file operation, where a native one
// costs about 0.02" is not an argument anyone has to take on faith, and it is
// the number that explains why a PHP page takes seconds.

const (
	// probeOps is how many file operations to time. Enough that the result is
	// not dominated by the 10 ms clock resolution available inside a container
	// (a slow mount spends around 400 ms on this), few enough that the probe
	// itself stays under a second on a fast one.
	probeOps = 200
	// probeDir names the scratch directory created inside the measured directory
	// and removed again, prefixing a per-run suffix so two doctors cannot collide.
	// Named so that a copy left behind by a killed probe is obviously Hull's.
	probeDir = ".hull-mount-probe"
	// probeDirPlaceholder is substituted for the real directory name at run time.
	probeDirPlaceholder = "__HULL_PROBE_DIR__"
	// probeTimeout bounds the whole thing including an image pull.
	probeTimeout = 90 * time.Second
	// slowMsPerOp is where a mount stops being a detail and starts being the
	// reason pages take seconds. A 9p mount measures around 1 to 4 ms/op
	// depending on what else the machine is doing; a native one is far below
	// this, and so is a WSL distro reached through Docker's mount service.
	slowMsPerOp = 0.5
	// resolutionMsPerOp is the smallest per-operation cost the probe can
	// distinguish, given a 10 ms clock over probeOps operations. Anything below
	// it is reported as "under", because claiming a precise number there would
	// be inventing precision the measurement does not have.
	resolutionMsPerOp = 10.0 / probeOps
)

// MountSpeed is what one probe run learned about a directory.
type MountSpeed struct {
	// MsPerOp is the measured cost of a file operation on the directory.
	MsPerOp float64
	// NativeMsPerOp is the same measurement taken inside the container's own
	// filesystem on the same run, which is the only fair comparison: same
	// machine, same kernel, same code. It is the floor Hull is arguing the
	// mount should be near.
	NativeMsPerOp float64
	// Writable is false when the directory could not be measured because a
	// container cannot write to it, which is an answer rather than a failure.
	Writable bool
}

// Ratio reports how many times more expensive the mount is than the container's
// own filesystem, clamped to the measurable range so it never reports infinity
// when the native side is too fast to time.
func (m MountSpeed) Ratio() float64 {
	native := m.NativeMsPerOp
	if native < resolutionMsPerOp {
		native = resolutionMsPerOp
	}
	return m.MsPerOp / native
}

// NativeText renders the native figure, saying "under" rather than inventing a
// number the clock could not have produced.
func (m MountSpeed) NativeText() string {
	if m.NativeMsPerOp < resolutionMsPerOp {
		return fmt.Sprintf("under %.2f", resolutionMsPerOp)
	}
	return fmt.Sprintf("%.2f", m.NativeMsPerOp)
}

// probeScript creates a known number of files on the mount, stats them once to
// warm any cache, then times a second pass.
//
// Timing comes from /proc/uptime because busybox's date has no %N, so the
// obvious `date +%s%N` silently returns zero and every measurement reads as
// instant. Resolution is 10 ms, which over 200 operations is 0.05 ms/op.
var probeScript = `set -e
now() { cut -d' ' -f1 /proc/uptime | tr -d '.'; }
n=` + strconv.Itoa(probeOps) + `
measure() {
  d=$1
  rm -rf "$d" 2>/dev/null || true
  mkdir -p "$d" 2>/dev/null || return 1
  i=0; while [ $i -lt $n ]; do : > "$d/f$i"; i=$((i+1)); done
  stat -c %Y "$d"/f* > /dev/null 2>&1
  s=$(now); stat -c %Y "$d"/f* > /dev/null 2>&1; e=$(now)
  rm -rf "$d" 2>/dev/null || true
  echo $((e-s))
}
mount=$(measure /probe/` + probeDirPlaceholder + `) || { echo HULL_PROBE_READONLY; exit 0; }
native=$(measure /tmp/` + probeDirPlaceholder + `) || native=0
echo "HULL_PROBE_CS=$mount HULL_NATIVE_CS=$native"`

// MountProbe measures the per-file-operation cost of a directory as a container
// sees it, in milliseconds. It reports 0 with a nil error when the directory is
// not writable from the container, which is a legitimate answer rather than a
// failure.
//
// It runs through the injected Output so it inherits the same quiet-spawn
// treatment as every other docker call, and it is deliberately tolerant: a
// machine with no engine, no image and no network is a machine where doctor
// still has to finish and report everything else.
func MountProbe(ctx context.Context, deps Deps, dir string) (MountSpeed, error) {
	if deps.Output == nil {
		return MountSpeed{}, fmt.Errorf("no docker probe available")
	}
	image, err := probeImage(ctx, deps)
	if err != nil {
		return MountSpeed{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// Docker's -v parser splits on colons, so a Windows path has to arrive with
	// forward slashes or W:\Sites misparses into its drive letter and the rest.
	mount := filepath.ToSlash(dir) + ":/probe"
	// Two doctors running at once (a terminal and the GUI, say) would otherwise
	// use the same scratch directory: the first to finish deletes it out from
	// under the second, which then reports the root as unwritable. The pid makes
	// each run's directory its own, and a leftover from a killed probe is still
	// obviously Hull's.
	out, err := deps.Output(ctx, "", "docker", "run", "--rm", "-v", mount, image, "sh", "-c",
		strings.ReplaceAll(probeScript, probeDirPlaceholder, fmt.Sprintf("%s-%d", probeDir, os.Getpid())))
	if err != nil {
		return MountSpeed{}, fmt.Errorf("running the mount probe: %w", err)
	}
	if strings.Contains(out, "HULL_PROBE_READONLY") {
		return MountSpeed{}, nil
	}
	return parseProbe(out)
}

// parseProbe turns the probe's centisecond counts into milliseconds per
// operation.
func parseProbe(out string) (MountSpeed, error) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "HULL_PROBE_CS=") {
			continue
		}
		mount, err1 := strconv.Atoi(strings.TrimPrefix(fields[0], "HULL_PROBE_CS="))
		native, err2 := strconv.Atoi(strings.TrimPrefix(fields[1], "HULL_NATIVE_CS="))
		if err1 != nil || err2 != nil || mount < 0 || native < 0 {
			return MountSpeed{}, fmt.Errorf("unreadable probe result %q", line)
		}
		return MountSpeed{
			MsPerOp:       float64(mount) * 10 / probeOps,
			NativeMsPerOp: float64(native) * 10 / probeOps,
			Writable:      true,
		}, nil
	}
	return MountSpeed{}, fmt.Errorf("the mount probe produced no result")
}

// probeImages are tried in order. Anything with a POSIX shell and stat will do,
// so the list is just "smallest thing likely to already be here"; the last entry
// is the fallback Docker will pull if none of the others are present.
var probeImages = []string{"alpine:latest", "busybox:latest", "nginx:alpine", "alpine"}

// probeImage picks an image that is already on the machine, so the common case
// costs nothing. Falls back to pulling the smallest one.
func probeImage(ctx context.Context, deps Deps) (string, error) {
	lookCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, img := range probeImages[:len(probeImages)-1] {
		if _, err := deps.Output(lookCtx, "", "docker", "image", "inspect", "--format", "ok", img); err == nil {
			return img, nil
		}
	}
	return probeImages[len(probeImages)-1], nil
}
