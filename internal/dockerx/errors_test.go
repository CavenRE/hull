package dockerx

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCommandErrorClassifiesEngineDown(t *testing.T) {
	// Real messages users hit. The Windows npipe one is verbatim from the bug
	// report that prompted this.
	engineDown := []string{
		`failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine; check if the path is correct and if the daemon is running: open //./pipe/dockerDesktopLinuxEngine: The system cannot find the file specified.`,
		`Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?`,
		`error during connect: Get "http://%2F%2F.%2Fpipe%2FdockerDesktopLinuxEngine/v1.24/version": open //./pipe/dockerDesktopLinuxEngine: The system cannot find the file specified.`,
	}
	for _, msg := range engineDown {
		err := commandError("docker", []string{"ps"}, msg, errors.New("exit status 1"))
		if !errors.Is(err, ErrEngineDown) {
			t.Errorf("not classified as engine-down: %s", msg[:60])
		}
		// The raw transport detail must not reach the user.
		if strings.Contains(err.Error(), "npipe") || strings.Contains(err.Error(), "docker.sock") {
			t.Errorf("raw transport detail leaked: %s", err)
		}
		if !strings.Contains(err.Error(), EngineDownHint) {
			t.Errorf("missing actionable hint: %s", err)
		}
	}
}

func TestCommandErrorKeepsRealFailures(t *testing.T) {
	// A genuine command failure must keep docker's own message: that is the
	// useful part, and misclassifying it as engine-down would send the user off
	// restarting Docker for no reason.
	msg := "no such service: web"
	err := commandError("docker", []string{"compose", "up"}, msg, errors.New("exit status 1"))
	if errors.Is(err, ErrEngineDown) {
		t.Error("a real failure was misclassified as engine-down")
	}
	if !strings.Contains(err.Error(), msg) {
		t.Errorf("lost docker's message: %s", err)
	}
}

func TestCommandErrorTruncatesLongOutput(t *testing.T) {
	long := strings.Repeat("x", 2000)
	err := commandError("docker", []string{"build"}, long, errors.New("exit status 1"))
	if len(err.Error()) > 1000 {
		t.Errorf("error not truncated: %d chars", len(err.Error()))
	}
}

func TestCommandErrorFallsBackToExitError(t *testing.T) {
	err := commandError("docker", []string{"ps"}, "   ", fmt.Errorf("exit status 127"))
	if !strings.Contains(err.Error(), "exit status 127") {
		t.Errorf("empty stderr should fall back to the exit error: %s", err)
	}
}
