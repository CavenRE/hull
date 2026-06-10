// Command hulld is the Hull daemon: it owns the project engine behind a
// local HTTP API on 127.0.0.1, guarded by a bearer token in
// ~/.hull/daemon.json (ADR 0006). Service-manager integration (systemd,
// launchd, Windows service) arrives in Phase 4.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logf := func(format string, a ...any) {
		fmt.Printf(format+"\n", a...)
	}
	if err := api.Serve(ctx, cfg, logf); err != nil {
		fmt.Fprintln(os.Stderr, "hulld:", err)
		os.Exit(1)
	}
}
