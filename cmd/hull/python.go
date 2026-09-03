package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// pythonExecCmd builds a `hull <bin> <args...>` command that runs <bin> inside
// the current project's app container (the python template puts the venv's
// python and pip first on PATH). Mirrors `hull artisan`.
func pythonExecCmd(bin, short, long, example string) *cobra.Command {
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
	rootCmd.AddCommand(pythonExecCmd("python",
		"Run python in the current project's app container",
		"Run python inside the current project's running app container, using the\n"+
			"venv on the project's named volume. With no arguments it opens a REPL.\n\n"+
			"Run it from inside a python project; the container must be up (hull up).\n"+
			"This is the python analog of hull artisan: a shortcut for hull exec python.",
		"  hull python                 (open a REPL)\n"+
			"  hull python manage.py migrate\n"+
			"  hull python -m http.server"))

	rootCmd.AddCommand(pythonExecCmd("pip",
		"Run pip in the current project's app container",
		"Run pip inside the current project's app container, installing into the\n"+
			"venv on the project's named volume (kept off the slow bind mount).\n\n"+
			"Add a package to requirements.txt as well so it persists across a rebuild.\n"+
			"Run it from inside a python project; the container must be up (hull up).",
		"  hull pip install requests\n"+
			"  hull pip freeze"))
}
