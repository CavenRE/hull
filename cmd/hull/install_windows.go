//go:build windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/version"
)

var (
	modkernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = modkernel32.NewProc("GetConsoleProcessList")
)

// maybeRunAsInstaller detects a double-click launch from Explorer (a fresh
// console with only this process attached) with no CLI arguments, and runs the
// install flow with prompts so the user does not have to open a terminal.
// Returns true if it handled the launch.
func maybeRunAsInstaller() bool {
	if len(os.Args) != 1 || !launchedFromExplorer() {
		return false
	}
	fmt.Println("  Hull installer")
	fmt.Println("  =============")
	fmt.Println()
	fmt.Printf("  This installs Hull to %s and adds it to your PATH.\n", defaultInstallDir())
	fmt.Print("  Press Enter to install, or close this window to cancel... ")
	waitEnter()
	fmt.Println()
	if err := runInstall(""); err != nil {
		fmt.Fprintf(os.Stderr, "\n  Install failed: %v\n", err)
	}
	fmt.Print("\n  Press Enter to close... ")
	waitEnter()
	return true
}

// launchedFromExplorer reports whether we own a fresh console with no other
// process attached, the signature of a double-click (a launch from an existing
// terminal shares that terminal's console with the shell, so the count is >1).
func launchedFromExplorer() bool {
	var pids [4]uint32
	r, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return r == 1
}

func waitEnter() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n') }

func defaultInstallDir() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "Hull") }

// runInstall copies the running hull.exe into the install directory, adds it to
// PATH, and registers an Apps & Features entry. Re-running updates the copy in
// place, stopping a running daemon first so its lock on hull.exe releases.
func runInstall(dir string) error {
	if dir == "" {
		dir = defaultInstallDir()
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	target := filepath.Join(dir, "hull.exe")

	if strings.EqualFold(self, target) {
		fmt.Printf("Hull is already installed at %s; refreshing PATH + registry.\n", dir)
		if err := addToUserPath(dir); err != nil {
			return err
		}
		return writeUninstallEntry(dir)
	}

	fmt.Printf("Installing Hull to %s\n", dir)
	// A running daemon is a hull.exe at target and locks the file; stop it.
	if a, err := loadApp(); err == nil {
		if _, ok := api.Connect(a.Config.HullHome); ok {
			fmt.Println("  stopping the running daemon...")
			stopDaemonAndWait(context.Background(), a.Config.HullHome)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	staged, err := stageReplacement(target, self)
	if err != nil {
		return fmt.Errorf("copying hull.exe: %w", err)
	}
	if err := commitReplacement(target, staged); err != nil {
		return fmt.Errorf("installing hull.exe: %w", err)
	}
	if err := addToUserPath(dir); err != nil {
		return fmt.Errorf("PATH: %w", err)
	}
	if err := writeUninstallEntry(dir); err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	// Actually finish the job. Printing "now go run setup" left fresh installs
	// with no router, no hosts block, and no daemon, so nothing was ever served.
	// Setup itself provisions the CA/DNS, starts the daemon, and enables
	// start-at-login, so hand off to the copy we just installed.
	fmt.Println("\nRunning setup (this may prompt for elevation)...")
	setup := exec.Command(target, "setup")
	setup.Stdin, setup.Stdout, setup.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := setup.Run(); err != nil {
		fmt.Println("\n! Setup did not finish:", err)
		fmt.Println("  Open a NEW terminal and run: hull setup")
		return nil
	}
	fmt.Println("\nHull is installed and running. In a NEW terminal try:")
	fmt.Println("  hull new demo laravel")
	return nil
}

func addToUserPath(dir string) error {
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

// writeUninstallEntry registers Hull in Apps & Features (uninstallRegKey is
// defined in uninstall_windows.go, which reads it back on removal).
func writeUninstallEntry(dir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallRegKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	unstr := `"` + filepath.Join(dir, "hull.exe") + `" uninstall --quiet`
	_ = k.SetStringValue("DisplayName", "Hull")
	_ = k.SetStringValue("DisplayVersion", version.Version)
	_ = k.SetStringValue("Publisher", "CavenRE")
	_ = k.SetStringValue("InstallLocation", dir)
	_ = k.SetStringValue("DisplayIcon", filepath.Join(dir, "hull.exe"))
	_ = k.SetStringValue("UninstallString", unstr)
	_ = k.SetStringValue("QuietUninstallString", unstr)
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return nil
}
