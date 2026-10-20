package dbauth

import (
	"os"
	"runtime"
	"testing"
)

func TestEnsureIsStableAndPrivate(t *testing.T) {
	home := t.TempDir()
	if _, ok := Get(home, "instance/mysql-8.0", "shop"); ok {
		t.Fatal("login before Ensure")
	}
	a, err := Ensure(home, "instance/mysql-8.0", "shop", "shop")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Ensure(home, "instance/mysql-8.0", "shop", "other")
	if err != nil || a != b || len(a.Password) < 20 {
		t.Fatalf("second Ensure changed the login: %+v %+v %v", a, b, err)
	}
	if fi, _ := os.Stat(path(home, "instance/mysql-8.0", "shop")); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	if err := Forget(home, "instance/mysql-8.0"); err != nil {
		t.Fatal(err)
	}
	if _, ok := Get(home, "instance/mysql-8.0", "shop"); ok {
		t.Error("Forget kept the login")
	}
}
