package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Microsoft Defender scans every file read, which lands squarely on the hot
// path Hull already struggles with on Windows: a PHP request touching thousands
// of files pays the scan thousands of times. It is the most likely explanation
// for a page that usually takes 7 seconds occasionally taking 16.
//
// Hull deliberately does not change anyone's antivirus configuration. Adding an
// exclusion needs an elevated shell and is a real security decision, so Hull's
// job is to work out exactly which paths matter on this machine and hand over
// commands the user can read before running.

// DefenderPaths returns the directories worth excluding on this machine, in the
// order they matter: the project roots (scanned on every file the container
// reads), then Docker Desktop's own disk images, which are one enormous file
// each and are written constantly.
func DefenderPaths(roots []string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		seen[strings.ToLower(p)] = true
		paths = append(paths, p)
	}
	for _, r := range roots {
		// A WSL-hosted root is inside a disk image, not on NTFS, so Defender
		// never sees its individual files and excluding the UNC path does
		// nothing. The distro's image is covered below instead.
		if OnWindowsFilesystem(r) {
			add(r)
		}
	}
	for _, p := range dockerDataDirs() {
		add(p)
	}
	return paths
}

// DefenderCommands renders the exclusions as PowerShell the user can run in an
// elevated shell. Returns nil when there is nothing to exclude.
func DefenderCommands(roots []string) []string {
	paths := DefenderPaths(roots)
	if len(paths) == 0 {
		return nil
	}
	cmds := make([]string, 0, len(paths))
	for _, p := range paths {
		cmds = append(cmds, "Add-MpPreference -ExclusionPath '"+strings.ReplaceAll(p, "'", "''")+"'")
	}
	return cmds
}

// dockerDataDirs finds the directories holding Docker Desktop's WSL disk images
// and the images of any installed distribution. Both layouts Docker Desktop has
// shipped are probed (wsl\main and wsl\data), and only directories that exist
// are returned, so the advice never names a path this machine does not have.
func dockerDataDirs() []string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return nil
	}
	var dirs []string
	for _, candidate := range []string{
		filepath.Join(local, "Docker", "wsl"),
		filepath.Join(local, "wsl"), // where WSL keeps distro images by default
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			dirs = append(dirs, candidate)
		}
	}
	return dirs
}
