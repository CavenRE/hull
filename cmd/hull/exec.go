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
		Long: "Run an arbitrary command inside the current project's running app\n" +
			"container, streaming its output to your terminal.\n\n" +
			"It must be run from inside a project directory: Hull resolves the\n" +
			"project from the working directory and execs into its compose service\n" +
			"(the app service by default). The command and its arguments are passed\n" +
			"through unchanged, so quote or escape anything your shell would\n" +
			"otherwise interpret. The container must be up (start it with hull up).\n\n" +
			"Use --service to target a different compose service, for example a\n" +
			"queue worker or a scheduler container instead of the web app. For\n" +
			"Laravel artisan specifically, the hull artisan shortcut is shorter.",
		Example: "  hull exec composer install\n" +
			"  hull exec php -v\n" +
			"  hull exec --service worker php artisan queue:work",
		Args: cobra.MinimumNArgs(1),
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
	// Stop parsing hull's flags once the command begins, so they belong to the
	// executed command: `hull exec php --version` passes --version to php, not hull.
	cmd.Flags().SetInterspersed(false)
	rootCmd.AddCommand(cmd)
}

func init() {
	var service string
	cmd := &cobra.Command{
		Use:   "artisan <command...>",
		Short: "Run Laravel artisan in the current project",
		Long: "Run Laravel's artisan console inside the current project's app\n" +
			"container. This is a shortcut for hull exec php artisan <command...>.\n\n" +
			"It must be run from inside a project directory, and the project's\n" +
			"container must be up (start it with hull up). Hull resolves the project\n" +
			"from the working directory, prepends php artisan, and passes the rest of\n" +
			"your arguments through unchanged.\n\n" +
			"Use --service to run artisan in a different compose service than the\n" +
			"app, for example a dedicated queue worker container.",
		Example: "  hull artisan migrate\n" +
			"  hull artisan make:controller PostController\n" +
			"  hull artisan --service worker queue:work",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, ok := a.currentProject()
			if !ok {
				return fmt.Errorf("run inside a project directory")
			}
			return a.Engine.ExecIn(cmd.Context(), p, service, append([]string{"php", "artisan"}, args...)...)
		},
	}
	cmd.Flags().StringVar(&service, "service", "app", "compose service to run artisan in (e.g. a queue worker)")
	cmd.Flags().SetInterspersed(false) // flags after the subcommand pass through (hull artisan migrate --force)
	rootCmd.AddCommand(cmd)
}

func init() {
	var image string
	cmd := &cobra.Command{
		Use:   "npm <command...>",
		Short: "Run npm in an ephemeral Node container",
		Long: "Run npm inside a throwaway Node container, without needing Node\n" +
			"installed on your machine or a running project.\n\n" +
			"It runs docker run --rm -it against a Node image (node:20-alpine by\n" +
			"default), bind-mounting the current working directory into the\n" +
			"container and running npm there, so installs and builds write straight\n" +
			"back to your files. On Linux/macOS it maps your host user and group id\n" +
			"into the container so created files are owned by you, not root. The\n" +
			"container is removed when the command finishes.\n\n" +
			"Unlike hull exec, this does not use a project's app container and does\n" +
			"not require being inside a Hull project; it operates on whatever\n" +
			"directory you are in. Use --image to pick a different Node version.",
		Example: "  hull npm install\n" +
			"  hull npm run build\n" +
			"  hull npm --image node:22-alpine ci",
		Args: cobra.MinimumNArgs(1),
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
				image, "npm")
			dockerArgs = append(dockerArgs, args...)
			return dockerx.Exec(cmd.Context(), "", "docker", dockerArgs...)
		},
	}
	cmd.Flags().StringVar(&image, "image", "node:20-alpine", "Node image to run npm in")
	cmd.Flags().SetInterspersed(false) // flags after npm's subcommand pass through (hull npm run build --watch)
	rootCmd.AddCommand(cmd)
}
