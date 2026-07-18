package services

import (
	"context"
	"os"
	"testing"
)

func TestCanonical(t *testing.T) {
	home := t.TempDir()
	m := &Manager{HullHome: home, Aliases: map[string]string{"db": "mysql-8.4", "x": "somewhere"}}
	for _, name := range []string{"postgres-16", "mysql-8.4", "mysql-8.0", "x"} {
		if err := os.MkdirAll(m.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct{ in, want string }{
		{"postgres-16", "postgres-16"}, // exact on-disk name wins
		{"db", "mysql-8.4"},            // configured alias
		{"postgres", "postgres-16"},    // engine shorthand: sole postgres
		{"mysql", "mysql"},             // ambiguous engine (8.0 + 8.4) stays literal
		{"x", "x"},                     // real instance beats the x->somewhere alias
		{"ghost", "ghost"},             // unknown token unchanged
		{"", ""},                       // empty stays empty
	}
	for _, tc := range cases {
		if got := m.Canonical(tc.in); got != tc.want {
			t.Errorf("Canonical(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInstanceToSpec(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mysql-8.4", "mysql@8.4"},
		{"postgres-16", "postgres@16"},
		{"mariadb-lts", "mariadb@lts"},
		{"adminer", "adminer"}, // no version, unchanged
	}
	for _, tc := range cases {
		if got := InstanceToSpec(tc.in); got != tc.want {
			t.Errorf("InstanceToSpec(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStartResolvesAliasAndShorthand(t *testing.T) {
	f := &fake{}
	m := f.manager(t.TempDir())
	if _, err := m.Add(context.Background(), "redis", ""); err != nil {
		t.Fatal(err)
	}
	m.Aliases = map[string]string{"r": "redis"}
	if err := m.Start(context.Background(), "r"); err != nil {
		t.Errorf("Start(alias r) failed: %v", err)
	}
	if err := m.Start(context.Background(), "redis"); err != nil {
		t.Errorf("Start(shorthand redis) failed: %v", err)
	}
	if err := m.Start(context.Background(), "ghost"); err == nil {
		t.Error("Start(ghost) should still fail")
	}
}
