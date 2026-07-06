package main

import (
	"github.com/spf13/cobra"
)

func init() {
	var dir string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install this hull binary onto the system (copy to a stable location + PATH)",
		Long: "Install the hull binary you are running onto the system: copy it to a\n" +
			"stable location and put that on your PATH, so `hull` works from any\n" +
			"terminal. This is how the downloaded hull.exe installs itself on Windows.\n\n" +
			"There is a single Hull binary: the CLI and the daemon are the same exe\n" +
			"(the daemon is `hull daemon run`). Installing copies just that one file.\n\n" +
			"On Windows it installs to %LOCALAPPDATA%\\Hull, adds it to your user PATH,\n" +
			"and registers an Apps & Features entry (uninstall runs `hull uninstall`);\n" +
			"no admin needed. On Linux/macOS it copies to ~/.local/bin. Use --dir to\n" +
			"choose a different location. Re-running is safe: it updates the installed\n" +
			"copy in place (stopping the daemon first if it is holding the file).",
		Example: "  hull install\n" +
			"  hull install --dir D:\\Tools\\Hull",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "install directory (default: platform user location)")
	rootCmd.AddCommand(cmd)
}
