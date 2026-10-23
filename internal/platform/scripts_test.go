package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQuoting(t *testing.T) {
	if got := ShellQuote(`it's here`); got != `'it'\''s here'` {
		t.Errorf("ShellQuote = %s", got)
	}
	if got := PSQuote("O'Brien\u2019s"); got != "'O''Brien\u2019\u2019s'" {
		t.Errorf("PSQuote = %s", got)
	}
	if got := appleScriptString(`say "hi" \ bye`); got != `"say \"hi\" \\ bye"` {
		t.Errorf("appleScriptString = %s", got)
	}
}

func TestWindowsScriptsQuoteAndTag(t *testing.T) {
	reg := nrptRegisterScript("te'st", "127.0.0.2")
	if !strings.Contains(reg, "'.te''st'") || !strings.Contains(reg, "$_.Comment -eq 'Hull local TLD'") {
		t.Errorf("register script:\n%s", reg)
	}
	if un := nrptUnregisterScript("test"); !strings.Contains(un, "$_.Comment -eq 'Hull local TLD'") {
		t.Errorf("unregister removes rules Hull did not add:\n%s", un)
	}
}

func TestUnixElevationIsArgv(t *testing.T) {
	for _, st := range append(resolvedSteps("/etc/x", "/etc/x/hull-test.conf", "body"), nmSteps("/a", "/b", "/a/c", "/b/r", "rule")...) {
		if st.args[0] == "sh" || strings.Contains(strings.Join(st.args, " "), " -c ") {
			t.Errorf("step runs a shell: %v", st.args)
		}
		if st.args[0] == "tee" && st.stdin == "" {
			t.Errorf("tee without content: %v", st.args)
		}
	}
	if got := adminInstallShell("/tmp/hull-1", "ab12", "/etc/resolver/my test"); !strings.HasPrefix(got, "/bin/mkdir -p '/etc/resolver' && ") ||
		!strings.Contains(got, "/bin/cat '/tmp/hull-1' > ") || !strings.Contains(got, " = 'ab12' ]") || !strings.Contains(got, `/bin/mv -f "$t" '/etc/resolver/my test'`) {
		t.Errorf("admin shell = %s", got)
	}
}

func TestAdminInstallShellChecksTheBytesItInstalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a POSIX shell script")
	}
	for _, tool := range []string{"/usr/bin/shasum", "/usr/bin/mktemp", "/usr/bin/cut"} {
		if _, err := os.Stat(tool); err != nil {
			t.Skip("needs", tool)
		}
	}
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	if err := os.WriteFile(staged, []byte("nameserver 127.0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("nameserver 127.0.0.1\n"))
	good := hex.EncodeToString(sum[:])
	dest := filepath.Join(dir, "resolver", "my test")

	if err := exec.Command("/bin/sh", "-c", adminInstallShell(staged, strings.Repeat("0", 64), dest)).Run(); err == nil {
		t.Error("a changed staged file was installed")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("the refused install left the destination file")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "resolver", ".hull.*")); len(left) != 0 {
		t.Errorf("the refused install left %v", left)
	}

	if out, err := exec.Command("/bin/sh", "-c", adminInstallShell(staged, good, dest)).CombinedOutput(); err != nil {
		t.Fatalf("install failed: %v %s", err, out)
	}
	fi, err := os.Stat(dest)
	if data, _ := os.ReadFile(dest); err != nil || string(data) != "nameserver 127.0.0.1\n" || fi.Mode().Perm() != 0o644 {
		t.Errorf("installed %q (%v)", data, err)
	}
}
