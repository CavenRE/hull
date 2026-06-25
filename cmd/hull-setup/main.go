//go:build windows && installer

// Command hull-setup is Hull's own installer — a self-contained exe that
// embeds the binaries and installs them, with NO NSIS. It never copies itself
// to %TEMP%, so SRP/AppLocker policies can't block it. Uninstall is wired to
// `hull uninstall`, which already works from the install directory.
//
// Build:  go build -tags installer -o bin\Hull-Setup.exe ./cmd/hull-setup
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

func main() {
	dir := flag.String("dir", filepath.Join(os.Getenv("LOCALAPPDATA"), "Hull"), "install directory")
	silent := flag.Bool("silent", false, "install without prompts")
	noPath := flag.Bool("no-path", false, "don't add the CLI to PATH")
	noShortcut := flag.Bool("no-shortcut", false, "don't create shortcuts")
	autostart := flag.Bool("autostart", false, "launch Hull at login")
	launch := flag.Bool("launch", false, "open Hull after installing")
	flag.Parse()

	if !*silent {
		fmt.Printf("Install Hull %s into:\n  %s\n\n", version, *dir)
		fmt.Println("This will add the hull CLI to your PATH and create shortcuts.")
		fmt.Print("Proceed? [Y/n] ")
		var r string
		fmt.Scanln(&r)
		if s := strings.TrimSpace(strings.ToLower(r)); s == "n" || s == "no" {
			fmt.Println("Cancelled.")
			return
		}
	}

	if err := install(*dir, !*noPath, !*noShortcut, *autostart); err != nil {
		fmt.Fprintln(os.Stderr, "Install failed:", err)
		os.Exit(1)
	}

	fmt.Println("\nHull installed.")
	if *launch {
		_ = exec.Command(filepath.Join(*dir, "hull-gui.exe")).Start()
	} else if !*silent {
		fmt.Println("Launch it from the Start menu, or run `hull` in a new terminal.")
	}
}

func install(dir string, addPath, shortcuts, autostart bool) error {
	fmt.Println("Stopping any running Hull...")
	for _, p := range []string{"hull-gui.exe", "hulld.exe"} {
		_ = exec.Command("taskkill", "/F", "/IM", p).Run()
	}

	fmt.Println("Copying files...")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := extract(payload, dir); err != nil {
		return fmt.Errorf("extracting files: %w", err)
	}

	if addPath {
		fmt.Println("Adding the CLI to PATH...")
		if err := addToPath(dir); err != nil {
			fmt.Println("  note:", err)
		}
	}
	if shortcuts {
		fmt.Println("Creating shortcuts...")
		exe := filepath.Join(dir, "hull-gui.exe")
		mkShortcut(filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Hull.lnk`), exe)
		mkShortcut(filepath.Join(userHome(), "Desktop", "Hull.lnk"), exe)
	}

	fmt.Println("Registering uninstall...")
	if err := writeUninstallEntry(dir); err != nil {
		fmt.Println("  note:", err)
	}
	if autostart {
		setRun("Hull", `"`+filepath.Join(dir, "hull-gui.exe")+`"`)
	}
	return nil
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
