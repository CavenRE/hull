package platform

import (
	"runtime"
	"testing"
)

func TestOnWindowsFilesystem(t *testing.T) {
	// /mnt/<drive> is detected on every OS (a plain string check), so a project
	// on a Windows drive is still recognized when Hull runs inside WSL.
	for _, r := range []string{"/mnt/c/Sites/app", "/mnt/d/x"} {
		if !OnWindowsFilesystem(r) {
			t.Errorf("OnWindowsFilesystem(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"/home/me/app", "/mnt/wsl/x", "/mnt/", "/srv/c", "/mntfoo/c/x"} {
		if OnWindowsFilesystem(r) {
			t.Errorf("OnWindowsFilesystem(%q) = true, want false", r)
		}
	}
	// Drive letters are only recognized by the OS-specific VolumeName on Windows.
	if runtime.GOOS == "windows" && !OnWindowsFilesystem(`C:\Sites\app`) {
		t.Error(`OnWindowsFilesystem("C:\Sites\app") = false on windows, want true`)
	}
}
