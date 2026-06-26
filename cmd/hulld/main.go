// Command hulld is the Hull daemon: it owns the project engine behind a
// local HTTP API on 127.0.0.1, guarded by a bearer token in
// ~/.hull/daemon.json (ADR 0006). Service-manager integration (systemd,
// launchd, Windows service) arrives in Phase 4.
package main

import (
	"context"
	"fmt"
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

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("hulld", version.String())
		return
	}

	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "hulld:", err)
		os.Exit(1)
	}

	// The GUI spawns hulld detached with no console, so stdout vanishes.
	// Tee everything to ~/.hull/hulld.log so startup failures, panics, and
	// runtime errors are actually diagnosable.
	logw := io.Writer(os.Stdout)
	if f := openLog(cfg.HullHome); f != nil {
		defer f.Close()
		logw = io.MultiWriter(os.Stdout, f)
	}
	logger := log.New(logw, "", log.LstdFlags)

	// A panic in a detached daemon would otherwise disappear silently.
	defer func() {
		if r := recover(); r != nil {
			logger.Printf("PANIC: %v\n%s", r, debug.Stack())
			os.Exit(2)
		}
	}()

	logger.Printf("hulld %s starting (pid %d, home %s)", version.String(), os.Getpid(), cfg.HullHome)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, a ...any) { logger.Printf(format, a...) }
	if err := api.Serve(ctx, cfg, logf); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
	logger.Printf("hulld stopped cleanly")
}

// openLog opens ~/.hull/hulld.log for appending. It truncates first if the
// file has grown past ~1 MB so it never balloons unbounded. Returns nil on
// any failure , logging then falls back to stdout only, never fatal.
func openLog(home string) *os.File {
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
