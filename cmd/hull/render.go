package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/compose"
	"github.com/CavenRE/hull/internal/manifest"
)

func init() {
	var (
		out      string
		toStdout bool
	)
	cmd := &cobra.Command{
		Use:   "render [dir]",
		Short: "Regenerate compose.yaml from a project's hull.yaml",
		Long:  "Render the compose file for a project (default: current directory).\nBy default the result is written to compose.yaml in the project.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			m, err := manifest.Load(dir)
			if err != nil {
				return err
			}
			f, err := compose.Render(m, a.Engine.ComposeContext())
			if err != nil {
				return err
			}
			data, err := compose.Marshal(f)
			if err != nil {
				return err
			}

			if toStdout {
				_, err = os.Stdout.Write(data)
				return err
			}
			target := out
			if target == "" {
				target = filepath.Join(dir, "compose.yaml")
			}
			if err := os.WriteFile(target, data, 0o644); err != nil {
				return err
			}
			fmt.Println("Wrote", target)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "output path (default <dir>/compose.yaml)")
	cmd.Flags().BoolVar(&toStdout, "stdout", false, "print to stdout instead of writing a file")
	rootCmd.AddCommand(cmd)
}
