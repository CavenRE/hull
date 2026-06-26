//go:build linux && installer

// Command hull-setup (Linux) is Hull's graphical installer , the counterpart to
// the Windows Hull-Setup.exe. It's a single self-contained binary that embeds
// the daemon, CLI, GUI and icons, and installs them per-user (into ~/.local/bin
// plus the XDG desktop dirs). Run with no arguments it opens a Hull-themed
// WebView (WebKitGTK) window with a CLI-only vs CLI+GUI choice; --silent installs
// headless for scripting. Uninstall is wired to `hull uninstall`.
//
// Build:  ./build.sh           (stages payload.tgz, then builds with -tags installer)
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/CavenRE/hull/internal/platform"
)

//go:embed payload.tgz
var payload []byte

// InstallOpts are the choices collected by the GUI (or flags). GUI=false
// installs the CLI + daemon only (no desktop app, no menu launcher).
type InstallOpts struct {
	Dir       string
	GUI       bool
	AddPath   bool
	Menu      bool // application-menu launcher (the Linux "shortcut")
	Autostart bool // launch the GUI at login
	Service   bool // run hulld as a systemd --user service
}

func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".local", "bin")
}

func main() {
	dir := flag.String("dir", defaultDir(), "install directory for the binaries")
	silent := flag.Bool("silent", false, "install headless, no window (scripting)")
	noGUI := flag.Bool("no-gui", false, "CLI only , skip the desktop app")
	noPath := flag.Bool("no-path", false, "don't add the CLI to PATH")
	noMenu := flag.Bool("no-menu", false, "don't add an application-menu launcher")
	autostart := flag.Bool("autostart", false, "launch Hull at login")
	service := flag.Bool("service", false, "run hulld as a systemd --user service")
	launch := flag.Bool("launch", false, "open Hull after installing")
	uninstall := flag.Bool("uninstall", false, "remove Hull (delegates to `hull uninstall`)")
	flag.Parse()

	if *uninstall {
		runUninstall(*dir)
		return
	}

	opts := InstallOpts{
		Dir: *dir, GUI: !*noGUI, AddPath: !*noPath,
		Menu: !*noMenu, Autostart: *autostart, Service: *service,
	}

	if *silent {
		if err := install(opts, func(string, int) {}); err != nil {
			fmt.Fprintln(os.Stderr, "install failed:", err)
			os.Exit(1)
		}
		if *launch {
			launchHull(opts.Dir)
		}
		return
	}
	runGUI(opts) // Hull-themed WebKitGTK installer (gui_linux.go)
}

// install performs the install, reporting progress (0–100) via report.
func install(o InstallOpts, report func(msg string, pct int)) error {
	if o.Dir == "" {
		o.Dir = defaultDir()
	}

	report("Stopping any running Hull…", 5)
	for _, p := range []string{"hull-gui", "hulld"} {
		_ = exec.Command("pkill", "-x", p).Run()
	}

	report("Unpacking…", 20)
	stage, err := os.MkdirTemp("", "hull-setup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := extractTarGz(payload, stage); err != nil {
		return fmt.Errorf("unpacking: %w", err)
	}

	report("Copying files…", 40)
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return err
	}
	// CLI + daemon always; the desktop app only for a GUI install.
	bins := []string{"hull", "hulld"}
	if o.GUI {
		bins = append(bins, "hull-gui")
	}
	for _, b := range bins {
		if err := copyFile(filepath.Join(stage, b), filepath.Join(o.Dir, b), 0o755); err != nil {
			return fmt.Errorf("installing %s: %w", b, err)
		}
	}

	report("Allowing the daemon to bind ports 80/443…", 55)
	// Best-effort: a polkit prompt. If declined, the daemon can still run as
	// root or with a lowered unprivileged-port sysctl , so don't fail the install.
	if err := platform.GrantPortBind(filepath.Join(o.Dir, "hulld")); err != nil {
		report("(skipped port permission , grant it later with setcap)", 55)
	}

	if o.AddPath {
		report("Adding the CLI to PATH…", 65)
		if _, err := platform.AddToShellPath(o.Dir); err != nil {
			return fmt.Errorf("PATH: %w", err)
		}
	}

	// Desktop integration is GUI-only. Always reconcile both ways so a
	// full→CLI-only reinstall removes a previously-installed launcher/icons.
	if o.GUI && o.Menu {
		report("Creating the menu launcher…", 78)
		icons, err := readIcons(stage)
		if err == nil {
			_ = platform.InstallIcons(icons)
		}
		if err := platform.WriteDesktopEntry(filepath.Join(o.Dir, "hull-gui")); err != nil {
			return fmt.Errorf("menu launcher: %w", err)
		}
	} else {
		// CLI-only: drop a desktop app left over from a previous GUI install,
		// so a full→CLI-only reinstall doesn't leave a dangling launcher/binary.
		platform.RemoveDesktopEntry()
		platform.RemoveIcons()
		_ = os.Remove(filepath.Join(o.Dir, "hull-gui"))
	}

	if o.GUI && o.Autostart {
		report("Enabling launch at login…", 85)
		_ = platform.WriteAutostartEntry(filepath.Join(o.Dir, "hull-gui"))
	} else {
		platform.RemoveAutostartEntry()
	}

	if o.Service {
		report("Installing the background service…", 92)
		if err := platform.WriteSystemdUserUnit(filepath.Join(o.Dir, "hulld")); err == nil {
			_ = platform.EnableSystemdUserUnit()
		}
	}

	report("Done", 100)
	return nil
}

// runUninstall delegates to the installed `hull uninstall`, which knows how to
// reverse everything (binaries, launcher, icons, service, PATH).
func runUninstall(dir string) {
	hull := filepath.Join(dir, "hull")
	if _, err := os.Stat(hull); err != nil {
		hull = "hull" // fall back to PATH
	}
	cmd := exec.Command(hull, "uninstall", "--quiet")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	_ = cmd.Run()
}

func launchHull(dir string) {
	_ = exec.Command(filepath.Join(dir, "hull-gui")).Start()
}

// readIcons pulls the hicolor PNGs out of the staged payload, keyed by the
// hicolor size directory expected by platform.InstallIcons.
func readIcons(stage string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, size := range []string{"32x32", "128x128", "256x256", "512x512"} {
		b, err := os.ReadFile(filepath.Join(stage, "icons", size+".png"))
		if err != nil {
			continue // a missing size is fine
		}
		out[size] = b
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no icons in payload")
	}
	return out, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// extractTarGz unpacks a gzipped tar payload into dir, preserving file modes
// (so the executables stay executable).
func extractTarGz(data []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// Guard against path traversal in the archive.
		clean := filepath.Clean(hdr.Name)
		if filepath.IsAbs(clean) || clean == ".." || hasDotDotPrefix(clean) {
			return fmt.Errorf("unsafe path in payload: %s", hdr.Name)
		}
		dst := filepath.Join(dir, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}

func hasDotDotPrefix(p string) bool {
	return len(p) >= 3 && p[0] == '.' && p[1] == '.' && (p[2] == '/' || p[2] == os.PathSeparator)
}
