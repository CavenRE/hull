package ledger

import "testing"

func TestLedgerRoundTrip(t *testing.T) {
	home := t.TempDir()

	if got := List(home); len(got) != 0 {
		t.Fatalf("fresh home should list nothing, got %v", got)
	}

	if err := Add(home, Entry{Name: "a", Dir: "/x", Kind: "site"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(home, Entry{Name: "b", Dir: "/y", Kind: "cluster", ComposeRoot: "core", Profiles: []string{"dev"}}); err != nil {
		t.Fatal(err)
	}

	got := List(home)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("listed %v, want a,b sorted", got)
	}
	if got[1].ComposeRoot != "core" || len(got[1].Profiles) != 1 {
		t.Errorf("cluster fields not persisted: %+v", got[1])
	}

	// Add with an existing name upserts (no duplicate).
	if err := Add(home, Entry{Name: "a", Dir: "/z"}); err != nil {
		t.Fatal(err)
	}
	got = List(home)
	if len(got) != 2 {
		t.Fatalf("upsert changed the count: %v", got)
	}
	if got[0].Dir != "/z" {
		t.Errorf("upsert did not replace dir, got %q", got[0].Dir)
	}

	if err := Remove(home, "a"); err != nil {
		t.Fatal(err)
	}
	got = List(home)
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("after remove: %v", got)
	}

	// Removing an absent entry is a no-op, not an error.
	if err := Remove(home, "nope"); err != nil {
		t.Fatalf("remove of missing entry errored: %v", err)
	}
}
