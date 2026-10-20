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
	sddl := sd.String()
	user, _ := windows.GetCurrentProcessToken().GetTokenUser()
	for _, want := range []string{user.User.Sid.String(), "SY"} {
		if !strings.Contains(sddl, want) {
			t.Errorf("DACL %s lacks %s", sddl, want)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "{}" {
		t.Errorf("the owner can no longer read it: %v", err)
	}
}
