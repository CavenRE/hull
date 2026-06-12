package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/CavenRE/hull/internal/config"
)

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

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port

	if existing, ok := Connect(cfg.HullHome); ok {
		_ = ln.Close()
		st, _ := existing.Status(ctx)
		if st != nil {
			return fmt.Errorf("a daemon is already running (version %s)", st.Version)
		}
		return errors.New("a daemon is already running")
	}
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
		return err
	}
	defer stopNet()
	server.SyncRoutes = syncNow

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
