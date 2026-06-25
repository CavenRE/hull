//go:build windows && installer

// Command hull-setup is Hull's own installer — a self-contained exe that
// embeds the binaries and installs them, with NO NSIS. Double-clicking opens a
// Hull-themed WebView2 window (it's built as a GUI app, so it never shows a
// console); --silent runs headless for scripting. It never copies itself to
// %TEMP%, so SRP/AppLocker can't block it, and uninstall is wired to
// `hull uninstall`, which runs from the install directory.
//
// Build:  go build -tags installer -ldflags "-H windowsgui" -o bin\Hull-Setup.exe ./cmd/hull-setup
// (build.ps1 stages payload.zip first.)
package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

//go:embed payload.zip
var payload []byte

const (
	productName = "Hull"
	version     = "0.1.0"
	uninstKey   = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Hull`
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// InstallOpts are the choices collected by the GUI (or flags).
type InstallOpts struct {
	Dir       string
	AddPath   bool
	Shortcuts bool
	Autostart bool
}

func defaultDir() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "Hull") }

func main() {
	dir := flag.String("dir", defaultDir(), "install directory")
	silent := flag.Bool("silent", false, "install without the GUI (scripting)")
	noPath := flag.Bool("no-path", false, "don't add the CLI to PATH")
	noShortcut := flag.Bool("no-shortcut", false, "don't create shortcuts")
	autostart := flag.Bool("autostart", false, "launch Hull at login")
	launch := flag.Bool("launch", false, "open Hull after installing")
	flag.Parse()

	opts := InstallOpts{Dir: *dir, AddPath: !*noPath, Shortcuts: !*noShortcut, Autostart: *autostart}

	if *silent {
		if err := install(opts, func(string, int) {}); err != nil {
			os.Exit(1)
		}
		if *launch {
			launchHull(opts.Dir)
		}
		return
	}
	runGUI(opts) // Hull-themed WebView2 installer (gui.go)
}

// install performs the install, reporting progress (0–100) via report.
func install(o InstallOpts, report func(msg string, pct int)) error {
	if o.Dir == "" {
		o.Dir = defaultDir()
	}
	report("Stopping any running Hull…", 5)
	for _, p := range []string{"hull-gui.exe", "hulld.exe"} {
		_ = exec.Command("taskkill", "/F", "/IM", p).Run()
	}

	report("Copying files…", 25)
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return err
	}
	if err := extract(payload, o.Dir); err != nil {
		return fmt.Errorf("copying files: %w", err)
	}

	if o.AddPath {
		report("Adding the CLI to PATH…", 70)
		if err := addToPath(o.Dir); err != nil {
			return fmt.Errorf("PATH: %w", err)
		}
	}
	if o.Shortcuts {
		report("Creating shortcuts…", 80)
		exe := filepath.Join(o.Dir, "hull-gui.exe")
		mkShortcut(filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Hull.lnk`), exe)
		mkShortcut(filepath.Join(userHome(), "Desktop", "Hull.lnk"), exe)
	}

	report("Registering…", 90)
	if err := writeUninstallEntry(o.Dir); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	if o.Autostart {
		setRun("Hull", `"`+filepath.Join(o.Dir, "hull-gui.exe")+`"`)
	}
	report("Done", 100)
	return nil
}

func launchHull(dir string) {
	_ = exec.Command(filepath.Join(dir, "hull-gui.exe")).Start()
}

func extract(zipBytes []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		dst := filepath.Join(dir, f.Name)
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(dst, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, cErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if cErr != nil {
			return cErr
		}
	}
	return nil
}

func addToPath(dir string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	cur, _, _ := k.GetStringValue("Path")
	for _, p := range strings.Split(cur, ";") {
		if strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			return nil // already present
		}
	}
	next := dir
	if cur != "" {
		next = cur + ";" + dir
	}
	return k.SetExpandStringValue("Path", next)
}

func writeUninstallEntry(dir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	hull := `"` + filepath.Join(dir, "hull.exe") + `" uninstall --quiet`
	_ = k.SetStringValue("DisplayName", productName)
	_ = k.SetStringValue("DisplayVersion", version)
	_ = k.SetStringValue("Publisher", "CavenRE")
	_ = k.SetStringValue("InstallLocation", dir)
	_ = k.SetStringValue("DisplayIcon", filepath.Join(dir, "hull-gui.exe"))
	_ = k.SetStringValue("UninstallString", hull)
	_ = k.SetStringValue("QuietUninstallString", hull)
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return nil
}

func setRun(name, cmd string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue(name, cmd)
}

// mkShortcut creates a .lnk via WScript.Shell (no extra Go deps).
func mkShortcut(lnk, target string) {
	ps := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%q); $s.TargetPath=%q; $s.WorkingDirectory=%q; $s.Save()`,
		lnk, target, filepath.Dir(target))
	_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
}

func userHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("USERPROFILE")
}
