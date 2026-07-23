package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
)

func init() {
	var noCache bool
	rebuild := &cobra.Command{
		Use:   "rebuild [name]",
		Short: "Rebuild a project's images and bring it back up",
		Long: "Rebuild the project's Docker images and bring it back up, streaming the\n" +
			"build output as it goes. With no name it targets the project in the\n" +
			"current directory.\n" +
			"\n" +
			"Use this after changing a Dockerfile, bumping a base image, or editing\n" +
			"build-time dependencies so the running containers pick up the new image.\n" +
			"Data in named volumes is preserved; only the images are rebuilt.\n" +
			"\n" +
			"By default Docker reuses cached layers. Pass --no-cache to build every\n" +
			"layer from the base images, which is slower but resolves stale-cache\n" +
			"problems (for example a package that silently failed to reinstall).\n" +
			"\n" +
			"Runs through the daemon when one is up (the build job is streamed back),\n" +
			"otherwise in-process against the local engine.",
		Example: "  hull rebuild\n" +
			"  hull rebuild shop\n" +
			"  hull rebuild shop --no-cache",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			fmt.Printf("Rebuilding %s%s...\n", p.Name, map[bool]string{true: " (no cache)"}[noCache])
			if err := a.withDaemon(
				func(c *api.Client) error {
					job, err := c.RebuildProject(cmd.Context(), p.Name, noCache)
					if err != nil {
						return err
					}
					return streamJob(cmd.Context(), c, job)
				},
				func() error {
					return a.Engine.Rebuild(cmd.Context(), p, noCache)
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ %s rebuilt.\n", p.Name)
			return nil
		},
	}
	rebuild.Flags().BoolVar(&noCache, "no-cache", false, "build images from scratch (ignore layer cache)")
	rootCmd.AddCommand(rebuild)
}

func init() {
	var force bool
	reset := &cobra.Command{
		Use:   "reset [name]",
		Short: "Wipe a project's data volumes and start fresh",
		Long: "Wipe the project's named volumes (databases, caches) and start it again\n" +
			"from a clean slate. With no name it targets the project in the current\n" +
			"directory.\n" +
			"\n" +
			"Only named volumes are removed, so your code and any host bind-mounts are\n" +
			"left untouched: this resets database and cache state, not your source. Use\n" +
			"it to drop a corrupted database, clear seed data, or return to a known\n" +
			"empty starting point.\n" +
			"\n" +
			"Unless --force (or the global --yes) is given, Hull first lists the named\n" +
			"volumes it will delete and prompts for confirmation. The wipe runs as a\n" +
			"streamed job through the daemon when one is up, otherwise in-process.",
		Example: "  hull reset\n" +
			"  hull reset shop\n" +
			"  hull reset shop --force",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			if !force {
				vols, verr := a.projectVolumes(cmd.Context(), p)
				if verr != nil {
					fmt.Printf("Warning: could not list volumes for %s (%v); the reset will still wipe its named volumes.\n", p.Name, verr)
				} else if len(vols) > 0 {
					fmt.Printf("This deletes %d named volume(s) for %s:\n", len(vols), p.Name)
					for _, v := range vols {
						fmt.Printf("  - %s\n", v)
					}
				} else {
					fmt.Printf("Reset %s (no named volumes detected).\n", p.Name)
				}
				ok, err := confirm("Wipe this data and start fresh?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("Aborted.")
					return nil
				}
			}
			fmt.Printf("Resetting %s...\n", p.Name)
			if err := a.withDaemon(
				func(c *api.Client) error {
					job, err := c.ResetProject(cmd.Context(), p.Name)
					if err != nil {
						return err
					}
					return streamJob(cmd.Context(), c, job)
				},
				func() error {
					return a.Engine.Reset(cmd.Context(), p)
				},
			); err != nil {
				return err
			}
			fmt.Printf("✔ %s reset.\n", p.Name)
			return nil
		},
	}
	reset.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt (alias of --yes)")
	rootCmd.AddCommand(reset)
}
