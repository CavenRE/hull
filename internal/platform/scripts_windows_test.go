package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestHostsInstallScriptRuns runs the hosts installer for real (not
// elevated) against temp files, so its PowerShell is proven, not just read.
func TestHostsInstallScriptRuns(t *testing.T) {
	dir := t.TempDir()
	staged, dest := filepath.Join(dir, "staged's.txt"), filepath.Join(dir, "hosts")
	if err := os.WriteFile(staged, []byte("127.0.0.2 shop.test\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(sum string) error {
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(hostsInstallScript(staged, sum, dest)))
		return cmd.Run()
	}
	if err := run("0000"); err == nil {
		t.Fatal("installed content whose hash does not match")
	}
	if data, _ := os.ReadFile(dest); string(data) != "old\r\n" {
		t.Fatalf("a refused install changed the hosts file: %q", data)
	}
	sum := sha256.Sum256([]byte("127.0.0.2 shop.test\r\n"))
	if err := run(hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "127.0.0.2 shop.test\r\n" {
		t.Errorf("hosts = %q", data)
	}
	if data, _ := os.ReadFile(dest + ".hull-backup"); string(data) != "old\r\n" {
		t.Errorf("backup = %q", data)
	}
}

func TestElevatedLauncherParses(t *testing.T) {
	check := `$e = $null; [void][System.Management.Automation.Language.Parser]::ParseInput($env:HULL_LAUNCHER, [ref]$null, [ref]$e); exit $e.Count`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(check))
	cmd.Env = append(os.Environ(), "HULL_LAUNCHER="+elevatedLauncher(encodePowerShell("Write-Output 1")))
	if err := cmd.Run(); err != nil {
		t.Errorf("the UAC launcher does not parse: %v", err)
	}
}
