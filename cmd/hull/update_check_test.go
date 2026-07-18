package main

import "testing"

func TestSemverNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
		why             string
	}{
		{"v0.15.2", "v0.15.1", true, "patch bump"},
		{"v0.16.0", "v0.15.9", true, "minor bump beats a higher patch"},
		{"v1.0.0", "v0.99.99", true, "major bump"},
		{"v0.15.1", "v0.15.1", false, "same version never prompts"},
		{"v0.15.0", "v0.15.1", false, "older release never prompts"},
		{"v0.15.1", "dev", false, "a dev build is never nagged"},
		{"", "v0.15.1", false, "no known release, no prompt"},
		{"v0.15.1", "v0.15.1-3-gabc1234", false, "dev build ahead of the tag stays quiet"},
		{"v0.16.0", "v0.15.1-3-gabc1234", true, "dev build behind a real release is offered"},
	}
	for _, c := range cases {
		if got := semverNewer(c.latest, c.current); got != c.want {
			t.Errorf("semverNewer(%q, %q) = %v, want %v (%s)", c.latest, c.current, got, c.want, c.why)
		}
	}
}

func TestParseSemver(t *testing.T) {
	if _, ok := parseSemver("dev"); ok {
		t.Error(`parseSemver("dev") should not parse`)
	}
	v, ok := parseSemver("v0.15.1-3-gabc1234")
	if !ok || v != [3]int{0, 15, 1} {
		t.Errorf("parseSemver(describe string) = %v, %v; want [0 15 1], true", v, ok)
	}
	if v, ok := parseSemver("1.2.3"); !ok || v != [3]int{1, 2, 3} {
		t.Errorf("parseSemver without leading v = %v, %v", v, ok)
	}
}
