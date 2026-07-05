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
		Long: "Regenerate a project's compose.yaml from its hull.yaml manifest. With no\n" +
			"directory it renders the project in the current directory.\n" +
			"\n" +
			"hull.yaml is the source of truth; compose.yaml is a generated artifact\n" +
			"that Hull normally re-renders on every up. Use render to inspect exactly\n" +
			"what Hull would produce, to refresh the compose file after hand-editing\n" +
			"the manifest, or to diff the two before starting anything.\n" +
			"\n" +
			"This runs entirely in-process and never talks to Docker or the daemon, so\n" +
			"it works offline and even when the engine is down. By default it writes\n" +
			"compose.yaml in the project; use --output to write elsewhere, or --stdout\n" +
			"to print the YAML without touching any file.",
		Example: "  hull render\n" +
			"  hull render ./shop\n" +
			"  hull render --stdout\n" +
			"  hull render ./shop -o /tmp/compose.yaml",
		Args: cobra.MaximumNArgs(1),
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
