package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHostsChecksHash(t *testing.T) {
	dir := t.TempDir()
	staged, dest := filepath.Join(dir, "staged's.txt"), filepath.Join(dir, "hosts")
	if err := os.WriteFile(staged, []byte("127.0.0.2 shop.test\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runElevatedOps([]string{"hosts", staged, "0000"}, dest); err == nil {
		t.Fatal("installed content whose hash does not match")
	}
	if data, _ := os.ReadFile(dest); string(data) != "old\r\n" {
		t.Fatalf("a refused install changed the hosts file: %q", data)
	}
	sum := sha256.Sum256([]byte("127.0.0.2 shop.test\r\n"))
	if err := runElevatedOps([]string{"hosts", staged, strings.ToUpper(hex.EncodeToString(sum[:]))}, dest); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "127.0.0.2 shop.test\r\n" {
		t.Errorf("hosts = %q", data)
	}
	if data, _ := os.ReadFile(dest + ".hull-backup"); string(data) != "old\r\n" {
		t.Errorf("backup = %q", data)
	}
}

func TestElevatedOpsRejectUnknownAndShort(t *testing.T) {
	for _, ops := range [][]string{{"format-disk"}, {"hosts", "only-one-arg"}, {"nrpt-add", "test"}, {"symlink", filepath.Join(t.TempDir(), "l"), `C:\Windows`}} {
		if err := runElevatedOps(ops, filepath.Join(t.TempDir(), "hosts")); err == nil {
			t.Errorf("ops %q ran", ops)
		}
	}
}

func TestRunPowerShellExitCode(t *testing.T) {
	if err := runPowerShell("$ErrorActionPreference = 'Stop'\nWrite-Output 'it''s'\n"); err != nil {
		t.Errorf("a good multi-line script failed: %v", err)
	}
	if err := runPowerShell("$ErrorActionPreference = 'Stop'\nthrow 'boom'\n"); err == nil {
		t.Error("a throwing script reported success")
	}
}
