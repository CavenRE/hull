package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// appBinCmd builds a `hull <bin> <args...>` command that runs <bin> inside the
// current project's app container (with that runtime's tools on PATH). Mirrors
// `hull artisan`, and backs `hull python`/`pip`/`node`/`go`.
func appBinCmd(bin, short, long, example string) *cobra.Command {
	var service string
	cmd := &cobra.Command{
		Use:     bin + " [args...]",
		Short:   short,
		Long:    long,
		Example: example,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, ok := a.currentProject()
			if !ok {
				return fmt.Errorf("run inside a project directory")
			}
			return a.Engine.ExecIn(cmd.Context(), p, service, append([]string{bin}, args...)...)
		},
	}
	cmd.Flags().StringVar(&service, "service", "app", "compose service to run in")
	// Flags after the subcommand belong to the executed command, not to hull:
	// `hull pip install -U requests` passes -U to pip.
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func init() {
	rootCmd.AddCommand(appBinCmd("python",
		"Run python in the current project's app container",
		"Run python inside the current project's running app container, using the\n"+
			"venv on the project's named volume. With no arguments it opens a REPL.\n\n"+
			"Run it from inside a python project; the container must be up (hull up).\n"+
			"This is the python analog of hull artisan: a shortcut for hull exec python.",
		"  hull python                 (open a REPL)\n"+
			"  hull python manage.py migrate\n"+
			"  hull python -m http.server"))

	rootCmd.AddCommand(appBinCmd("pip",
		"Run pip in the current project's app container",
		"Run pip inside the current project's app container, installing into the\n"+
			"venv on the project's named volume (kept off the slow bind mount).\n\n"+
			"Add a package to requirements.txt as well so it persists across a rebuild.\n"+
			"Run it from inside a python project; the container must be up (hull up).",
		"  hull pip install requests\n"+
			"  hull pip freeze"))

	rootCmd.AddCommand(appBinCmd("node",
		"Run node in the current project's app container",
		"Run node inside the current project's running app container. With no\n"+
			"arguments it opens a REPL. To manage packages, add them to package.json\n"+
			"and hull restart, or run hull exec npm install <pkg>.\n\n"+
			"Run it from inside a node project; the container must be up (hull up).",
		"  hull node                   (open a REPL)\n"+
			"  hull node server.js\n"+
			"  hull node --version"))

	rootCmd.AddCommand(appBinCmd("go",
		"Run the go toolchain in the current project's app container",
		"Run go inside the current project's running app container, which has the\n"+
			"toolchain and the module/build caches on named volumes. air already runs\n"+
			"the app, so this is for build/test/mod and other toolchain commands.\n\n"+
			"Run it from inside a go project; the container must be up (hull up).",
		"  hull go test ./...\n"+
			"  hull go mod tidy\n"+
			"  hull go get github.com/some/pkg"))
}
