package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/CavenRE/hull/internal/config"
	"github.com/CavenRE/hull/internal/dockerx"
)

// waitForDocker polls the Docker engine until it is reachable, so a cold-boot
// autostart (daemon up before Docker Desktop finishes starting) is not skipped.
// Bounded to ~60s; returns false on timeout or when ctx is canceled (shutdown).
func waitForDocker(ctx context.Context) bool {
	for i := 0; i < 30; i++ {
		if dockerx.EngineCheck(ctx) == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

// Serve runs the daemon on 127.0.0.1 with a fresh token, records the
// discovery file, and blocks until ctx is canceled or a client requests
// shutdown. Used by both hulld and `hull daemon run`.
func Serve(ctx context.Context, cfg *config.Config, logf func(format string, a ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	token, err := NewToken()
	if err != nil {
		return err
	}
	server := NewServer(cfg, token)

	// Single-daemon lock: refuse to start a second daemon (or take over a
	// stale lock from a crash) before touching the discovery file or ports.
	guard, err := acquireInstance(cfg.HullHome, func() bool {
		_, ok := Connect(cfg.HullHome)
		return ok
	})
	if err != nil {
		return err
	}
	defer guard.release()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	if err := WriteDaemonFile(cfg.HullHome, DaemonInfo{Port: port, Token: token, PID: os.Getpid()}); err != nil {
		_ = ln.Close()
		return err
	}
	defer RemoveDaemonFile(cfg.HullHome)

	var once sync.Once
	shutdownCh := make(chan struct{})
	server.OnShutdown = func() {
		once.Do(func() { close(shutdownCh) })
	}

	stopNet, syncNow, err := startNetworking(ctx, cfg, server.Engine, logf)
	if err != nil {
		_ = ln.Close()
		RemoveDaemonFile(cfg.HullHome)
		return fmt.Errorf("starting networking failed (a previous Hull daemon may still hold ports 80/443/53 , try `hull stop`): %w", err)
	}
	defer stopNet()
	server.SyncRoutes = syncNow

	// Bring up autostart projects and shared instances, then refresh routes so
	// they are served. Runs in the background so the listener is up immediately
	// even while `docker compose up` runs; best-effort (failures are logged).
	// Only spun up when something is actually marked, so the default (nothing
	// marked) never pays the wait-for-Docker cost.
	if server.Engine.HasAutostart() {
		go func() {
			// Docker Desktop may still be starting at login; wait for it so a
			// cold-boot autostart is not skipped. Bounded, and unblocks on
			// shutdown.
			if !waitForDocker(ctx) {
				logf("autostart: Docker did not become ready, skipping")
				return
			}
			if n, err := server.Engine.StartEnabled(ctx); err != nil {
				logf("autostart: %v", err)
			} else if n > 0 {
				logf("autostart: started %d item(s)", n)
			}
			syncNow()
		}()
	}

	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(ln) }()

	logf("hulld listening on 127.0.0.1:%d (home %s)", port, cfg.HullHome)

	select {
	case <-ctx.Done():
		logf("shutting down (signal)")
	case <-shutdownCh:
		logf("shutting down (client request)")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
