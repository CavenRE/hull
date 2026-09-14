package doctor

import (
	"strings"
	"testing"
)

func TestParseProbe(t *testing.T) {
	// 40 centiseconds over 200 operations is 2 ms each; 1 centisecond is the
	// floor the container's clock can report.
	speed, err := parseProbe("some image noise\nHULL_PROBE_CS=40 HULL_NATIVE_CS=1\n")
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	if speed.MsPerOp != 2 {
		t.Errorf("MsPerOp = %v, want 2", speed.MsPerOp)
	}
	if !speed.Writable {
		t.Error("a successful measurement must report the directory as writable")
	}
	if got := speed.Ratio(); got != 40 {
		t.Errorf("Ratio() = %v, want 40", got)
	}
}

// The native side is often faster than the clock can measure. Reporting it as
// "0.00 ms" would be inventing precision, and dividing by it would report an
// infinite ratio.
func TestParseProbeUnmeasurableNative(t *testing.T) {
	speed, err := parseProbe("HULL_PROBE_CS=40 HULL_NATIVE_CS=0")
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	if got := speed.NativeText(); !strings.HasPrefix(got, "under ") {
		t.Errorf("NativeText() = %q, want it to say 'under'", got)
	}
	ratio := speed.Ratio()
	if ratio <= 0 || ratio > 1000 {
		t.Errorf("Ratio() = %v, want a finite, sane number when the native side is too fast to time", ratio)
	}
}

func TestParseProbeRejectsGarbage(t *testing.T) {
	for _, out := range []string{
		"",
		"no marker here",
		"HULL_PROBE_CS=abc HULL_NATIVE_CS=1",
		"HULL_PROBE_CS=40",
		"HULL_PROBE_CS=-1 HULL_NATIVE_CS=1",
	} {
		if _, err := parseProbe(out); err == nil {
			t.Errorf("parseProbe(%q) succeeded, want an error", out)
		}
	}
}

// A read-only mount is an answer, not a failure: doctor must report it and move
// on rather than treating it as a broken machine.
func TestProbeScriptShape(t *testing.T) {
	if !strings.Contains(probeScript, "HULL_PROBE_READONLY") {
		t.Error("the probe script must be able to report an unwritable mount")
	}
	if !strings.Contains(probeScript, "/proc/uptime") {
		t.Error("the probe must time from /proc/uptime, because busybox date has no nanosecond format and silently returns zero")
	}
	if strings.Contains(probeScript, "date +%s%N") {
		t.Error("the probe must not use the date nanosecond format: it does not work in the probe image and measures everything as instant")
	}
}
