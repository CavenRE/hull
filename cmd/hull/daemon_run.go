package main

import (
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/version"
)

// runDaemon runs the Hull daemon in the foreground. It is the single daemon
// entrypoint (there is no separate hulld binary): `hull daemon run`, a systemd
// unit, or a detached launch all land here. Output is teed to ~/.hull/hulld.log
// with panic capture, since a service or detached process has no visible
// console, and it stops cleanly on Interrupt or SIGTERM.
func runDaemon(cfg *config.Config) error {
	logw := io.Writer(os.Stdout)
	if f := openDaemonLog(cfg.HullHome); f != nil {
		defer f.Close()
		logw = io.MultiWriter(os.Stdout, f)
	}
	logger := log.New(logw, "", log.LstdFlags)

	// A panic in a detached daemon would otherwise vanish.
	defer func() {
		if r := recover(); r != nil {
			logger.Printf("PANIC: %v\n%s", r, debug.Stack())
			os.Exit(2)
		}
	}()

	logger.Printf("hull daemon %s starting (pid %d, home %s)", version.String(), os.Getpid(), cfg.HullHome)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, a ...any) { logger.Printf(format, a...) }
	if err := api.Serve(ctx, cfg, logf); err != nil {
		logger.Printf("fatal: %v", err)
		return err
	}
	logger.Printf("hull daemon stopped cleanly")
	return nil
}

// openDaemonLog opens ~/.hull/hulld.log for appending, truncating first if it
// has grown past ~1 MB. Returns nil on any failure (logging then falls back to
// stdout only, never fatal).
func openDaemonLog(home string) *os.File {
	if home == "" {
		return nil
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil
	}
	path := filepath.Join(home, "hulld.log")
	if info, err := os.Stat(path); err == nil && info.Size() > 1<<20 {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return f
}
