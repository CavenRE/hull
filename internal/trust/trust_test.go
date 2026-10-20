package trust

import (
	"path/filepath"
	"testing"
)

func TestSetSeedRemove(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "shop")
	if Exists(home) {
		t.Fatal("store exists before first write")
	}
	if err := Seed(home, map[string]string{dir: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Seed(home, map[string]string{dir: "b"}); err != nil {
		t.Fatal(err)
	}
	if got := All(home)[Key(dir)]; got != "a" {
		t.Errorf("seed ran twice: fingerprint %q", got)
	}
	if err := Set(home, map[string]string{dir: "c"}); err != nil {
		t.Fatal(err)
	}
	if got := All(home)[Key(dir)]; got != "c" {
		t.Errorf("after Set: %q", got)
	}
	if err := Remove(home, dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := All(home)[Key(dir)]; ok {
		t.Error("Remove kept the entry")
	}
}
