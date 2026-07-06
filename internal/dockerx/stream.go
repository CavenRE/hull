package dockerx

import (
	"bufio"
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
	noWindow(cmd)
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
	noWindow(cmd)
	cmd.Stdin = r
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return commandError(name, args, errBuf.String(), err)
	}
	return nil
}

// StreamLines runs a command and delivers each stdout line to onLine until
// the command exits or ctx is canceled (container log following).
func StreamLines(ctx context.Context, dir string, onLine func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	noWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // interleave; docker logs writes to both
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil // client went away; not an error
	}
	return err
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
