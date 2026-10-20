package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRestrictToUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestrictToUser(path); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Error("the DACL still inherits from the folder")
	}
	// Built the same way, the expected DACL gets the same short names Windows
	// prints for well-known accounts (LA for the built-in Administrator, which
	// CI runs as), so the two compare as text.
	user, _ := windows.GetCurrentProcessToken().GetTokenUser()
	want, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	aces := func(sddl string) string { return sddl[strings.Index(sddl, "("):] }
	if got := sd.String(); aces(got) != aces(want.String()) {
		t.Errorf("DACL %s, want only full control for %s and SYSTEM: %s", got, user.User.Sid, want)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{}" {
		t.Errorf("the owner can no longer read it: %v", err)
	}
}
