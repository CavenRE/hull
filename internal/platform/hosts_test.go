package platform

import (
	"strings"
	"testing"
)

const herdStyleHosts = "# Odysseus\r\n127.0.0.4 odysseus.local\r\n\r\n# Herd generated Hosts. Do not change.\r\n127.0.0.1 demo.test\r\n# End Herd generated Hosts\r\n"

func TestHostsBlock(t *testing.T) {
	block := HostsBlock([]string{"beta.test", "alpha.test"}, "127.0.0.2", "test")
	want := HostsBegin + "\n127.0.0.2 alpha.test\n127.0.0.2 beta.test\n" + HostsEnd
	if block != want {
		t.Errorf("block = %q", block)
	}
	if HostsBlock(nil, "127.0.0.2", "test") != "" {
		t.Error("empty domains should produce no block")
	}
	// Empty ip falls back to 127.0.0.1.
	if !strings.Contains(HostsBlock([]string{"x.test"}, "", "test"), "127.0.0.1 x.test") {
		t.Error("empty ip should fall back to 127.0.0.1")
	}
}

func TestMergeAppendsWithoutTouchingOthers(t *testing.T) {
	merged := MergeHostsBlock(herdStyleHosts, HostsBlock([]string{"jane.test"}, "127.0.0.2", "test"))
	if !strings.Contains(merged, "# Herd generated Hosts. Do not change.") ||
		!strings.Contains(merged, "127.0.0.4 odysseus.local") {
		t.Errorf("foreign content lost:\n%s", merged)
	}
	if !strings.Contains(merged, HostsBegin+"\r\n127.0.0.2 jane.test\r\n"+HostsEnd) {
		t.Errorf("hull block missing:\n%s", merged)
	}
}

func TestMergeReplacesExistingBlock(t *testing.T) {
	first := MergeHostsBlock(herdStyleHosts, HostsBlock([]string{"old.test"}, "127.0.0.2", "test"))
	second := MergeHostsBlock(first, HostsBlock([]string{"new.test"}, "127.0.0.2", "test"))
	if strings.Contains(second, "old.test") {
		t.Errorf("old entry survived:\n%s", second)
	}
	if strings.Count(second, HostsBegin) != 1 {
		t.Errorf("duplicate blocks:\n%s", second)
	}
	if !strings.Contains(second, "127.0.0.2 new.test") {
		t.Errorf("new entry missing:\n%s", second)
	}
}

func TestMergeIdempotent(t *testing.T) {
	block := HostsBlock([]string{"a.test", "b.test"}, "127.0.0.2", "test")
	once := MergeHostsBlock(herdStyleHosts, block)
	twice := MergeHostsBlock(once, block)
	if once != twice {
		t.Error("merge is not idempotent")
	}
}

func TestMergeRemovesBlockWhenEmpty(t *testing.T) {
	withBlock := MergeHostsBlock(herdStyleHosts, HostsBlock([]string{"x.test"}, "127.0.0.2", "test"))
	removed := MergeHostsBlock(withBlock, "")
	if strings.Contains(removed, HostsBegin) || strings.Contains(removed, "x.test") {
		t.Errorf("block not removed:\n%s", removed)
	}
	if !strings.Contains(removed, "odysseus.local") {
		t.Error("foreign content lost on removal")
	}
}

func TestHostsBlockDropsPublicNames(t *testing.T) {
	block := HostsBlock([]string{"shop.test", "www.google.com"}, "127.0.0.2", "test")
	if strings.Contains(block, "google") || !strings.Contains(block, "shop.test") {
		t.Errorf("block:\n%s", block)
	}
	if HostsBlock([]string{"www.google.com"}, "127.0.0.2", "test") != "" {
		t.Error("a block was written for public names only")
	}
}

func TestMergeKeepsLinesAfterALoneBeginMarker(t *testing.T) {
	current := "127.0.0.1 localhost\n" + HostsBegin + "\n127.0.0.2 old.test\n10.0.0.5 nas.lan\n"
	merged := strings.ReplaceAll(MergeHostsBlock(current, HostsBlock([]string{"new.test"}, "127.0.0.2", "test")), "\r\n", "\n")
	if !strings.Contains(merged, "10.0.0.5 nas.lan") || !strings.Contains(merged, "127.0.0.1 localhost") {
		t.Errorf("lines after a lone marker were dropped:\n%s", merged)
	}
	if strings.Count(merged, HostsBegin) != 1 || !strings.Contains(merged, "127.0.0.2 new.test") {
		t.Errorf("block not written exactly once:\n%s", merged)
	}
}
