package dockerx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ExecCapture runs a command with stdout streamed to w (database dumps).
func ExecCapture(ctx context.Context, dir string, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = w
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return commandError(name, args, errBuf.String(), err)
	}
	return nil
}

// ExecStdin runs a command with stdin fed from r (database restores).
func ExecStdin(ctx context.Context, dir string, r io.Reader, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = r
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return commandError(name, args, errBuf.String(), err)
	}
	return nil
}

func commandError(name string, args []string, stderr string, err error) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	if len(msg) > 800 {
		msg = msg[:800] + "..."
	}
	return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
}
