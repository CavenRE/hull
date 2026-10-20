package templates

import (
	"context"
	"strings"
	"testing"
)

func TestScaffoldLaravelRefusesAShellInTheVersion(t *testing.T) {
	ran := false
	run := func(context.Context, string, string, ...string) error { ran = true; return nil }
	err := scaffoldLaravel(context.Background(), ScaffoldOptions{Dir: t.TempDir(), Version: "11; rm -rf /out", Run: run})
	if err == nil || !strings.Contains(err.Error(), "invalid laravel version") || ran {
		t.Errorf("err = %v, ran = %v", err, ran)
	}
}
