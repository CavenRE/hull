package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
)

func init() {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start Hull (the daemon) in the background",
		Long: "Start the Hull daemon (the router, DNS, and shared-services manager) as a\n" +
			"background process and return immediately, so your terminal stays free.\n\n" +
			"This is the headless counterpart to `hull daemon run`, which stays in the\n" +
			"foreground. Your projects are only served while the daemon is up, so this\n" +
			"is what makes `https://<name>.<tld>` reachable. `hull up` offers to run it\n" +
			"for you when it is not started yet.\n\n" +
			"If Hull is already running this is a no-op. Stop it with `hull stop`\n" +
			"(brings everything down) or `hull daemon stop` (just the daemon).",
		Example: "  hull start",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if _, ok := api.Connect(a.Config.HullHome); ok {
				fmt.Println("Hull is already running.")
				return nil
			}
			fmt.Println("Starting Hull...")
			if err := startDaemonDetached(cmd.Context(), a.Config.HullHome); err != nil {
				return err
			}
			fmt.Println("Hull is running.")
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}

// ensureDaemonRunning makes sure a daemon is up so the router can serve. When
// none is running (and --no-daemon was not given) it offers to start one, then
// starts it detached and waits. Declining is fine: the caller proceeds
// in-process (the container starts, but nothing serves it until Hull runs).
func ensureDaemonRunning(ctx context.Context, home string) error {
	if flagNoDaemon {
		return nil
	}
	if _, ok := api.Connect(home); ok {
		return nil
	}
	ok, err := confirm("Hull is not running, so your sites will not be served. Start it and continue?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("  Continuing without Hull. Run `hull start` when you want your sites served.")
		return nil
	}
	fmt.Println("Starting Hull...")
	if err := startDaemonDetached(ctx, home); err != nil {
		return fmt.Errorf("could not start Hull: %w", err)
	}
	fmt.Println("Hull is running.")
	return nil
}

// startDaemonDetached spawns `hull daemon run` as a detached background process
// (no console, survives this process) and waits until it answers.
func startDaemonDetached(ctx context.Context, home string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"daemon", "run"}
	if flagHome != "" {
		args = append(args, "--home", flagHome)
	}
	c := exec.Command(exe, args...)
	c.SysProcAttr = detachedSysProcAttr()
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	if err := c.Start(); err != nil {
		return err
	}
	_ = c.Process.Release()

	for i := 0; i < 100; i++ { // up to ~10s
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, ok := api.Connect(home); ok {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Hull started but is not responding yet (see ~/.hull/hulld.log)")
}
