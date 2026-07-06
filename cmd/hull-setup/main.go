//go:build windows && installer

// Command hull-setup is Hull's Windows installer, compiled to Hull.exe. It is a
// self-contained console app that embeds the CLI (hull.exe) and daemon
// (hulld.exe) as a zip payload, extracts them to %LOCALAPPDATA%\Hull, adds that
// to PATH, and registers an Apps & Features entry whose uninstall runs
// `hull uninstall`. Being a compiled exe (not a PowerShell script), it avoids
// the AMSI blocking that stops script-based installers on Windows.
//
// Build (build.ps1 -Installer stages payload.zip first):
//
//	go build -tags installer -ldflags "-s -w -X main.version=<ver>" -o dist\Hull.exe ./cmd/hull-setup
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

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

const (
	productName = "Hull"
	uninstKey   = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Hull`
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
)

func defaultDir() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "Hull") }

func main() {
	dir := flag.String("dir", defaultDir(), "install directory")
	silent := flag.Bool("silent", false, "run without the closing prompt (for scripting)")
	uninstall := flag.Bool("uninstall", false, "remove Hull instead of installing")
	flag.Parse()

	if *uninstall {
		doUninstall(*dir)
		pause(*silent)
		return
	}

	fmt.Printf("Installing Hull %s to %s\n\n", version, *dir)
	if err := install(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "\nInstall failed: %v\n", err)
		pause(*silent)
		os.Exit(1)
	}
	fmt.Println()
	fmt.Println("Hull is installed. Open a NEW terminal and run:")
	fmt.Println("  hull setup     # enable the router + DNS and trust the local CA")
	fmt.Println("  hull doctor    # verify Docker, ports, certs")
	fmt.Println("  hulld          # start the daemon, then: hull new demo laravel")
	pause(*silent)
}

func install(dir string) error {
	if dir == "" {
		dir = defaultDir()
	}
	step("Stopping any running Hull daemon")
	_ = exec.Command("taskkill", "/F", "/IM", "hulld.exe").Run()

	step("Copying files to " + dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := extract(payload, dir); err != nil {
		return fmt.Errorf("copying files: %w", err)
	}

	step("Adding the CLI to your PATH")
	if err := addToPath(dir); err != nil {
		return fmt.Errorf("PATH: %w", err)
	}

	step("Registering with Apps & Features")
	if err := writeUninstallEntry(dir); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	removeRun("Hull") // clear any stale autostart left by an old GUI install
	return nil
}

func doUninstall(dir string) {
	if dir == "" {
		dir = defaultDir()
	}
	hull := filepath.Join(dir, "hull.exe")
	if _, err := os.Stat(hull); err == nil {
		step("Removing Hull")
		if exec.Command(hull, "uninstall", "--quiet").Run() == nil {
			fmt.Println("Hull removed.")
			return
		}
	}
	// Fallback if the CLI's own uninstall is missing or failed.
	step("Removing Hull")
	removeFromPath(dir)
	deleteUninstallEntry()
	_ = os.RemoveAll(dir)
	fmt.Println("Hull removed.")
}

func step(msg string) { fmt.Printf("  %s...\n", msg) }

func pause(silent bool) {
	if silent {
		return
	}
	fmt.Print("\nPress Enter to close...")
	var s string
	_, _ = fmt.Scanln(&s)
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

func removeFromPath(dir string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	cur, _, _ := k.GetStringValue("Path")
	var keep []string
	for _, p := range strings.Split(cur, ";") {
		if p == "" || strings.EqualFold(strings.TrimRight(p, `\`), strings.TrimRight(dir, `\`)) {
			continue
		}
		keep = append(keep, p)
	}
	_ = k.SetExpandStringValue("Path", strings.Join(keep, ";"))
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
	_ = k.SetStringValue("DisplayIcon", filepath.Join(dir, "hull.exe"))
	_ = k.SetStringValue("UninstallString", hull)
	_ = k.SetStringValue("QuietUninstallString", hull)
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return nil
}

func deleteUninstallEntry() {
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstKey)
}

func removeRun(name string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(name)
}
