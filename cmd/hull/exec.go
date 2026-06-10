package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
)

func init() {
	var service string
	cmd := &cobra.Command{
		Use:   "exec <command...>",
		Short: "Run a command inside the current project's app container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, ok := a.currentProject()
			if !ok {
				return fmt.Errorf("run inside a project directory")
			}
			return a.Engine.ExecIn(cmd.Context(), p, service, args...)
		},
	}
	cmd.Flags().StringVar(&service, "service", "app", "compose service to exec into")
	rootCmd.AddCommand(cmd)
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "artisan <command...>",
		Short: "Run Laravel artisan in the current project",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, ok := a.currentProject()
			if !ok {
				return fmt.Errorf("run inside a project directory")
			}
			return a.Engine.ExecIn(cmd.Context(), p, "app", append([]string{"php", "artisan"}, args...)...)
		},
	})
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "npm <command...>",
		Short: "Run npm in an ephemeral Node container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			dockerArgs := []string{"run", "--rm", "-it"}
			if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 && gid >= 0 {
				dockerArgs = append(dockerArgs, "--user", fmt.Sprintf("%d:%d", uid, gid))
			}
			dockerArgs = append(dockerArgs,
				"-v", wd+":/usr/src/app",
				"-w", "/usr/src/app",
				"node:20-alpine", "npm")
			dockerArgs = append(dockerArgs, args...)
			return dockerx.Exec(cmd.Context(), "", "docker", dockerArgs...)
		},
	})
}
