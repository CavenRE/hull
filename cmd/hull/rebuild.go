package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/dockerx"
)

func init() {
	var noCache bool
	rebuild := &cobra.Command{
		Use:   "rebuild [name]",
		Short: "Rebuild a project's images and bring it back up",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			fmt.Printf("Rebuilding %s%s...\n", p.Name, map[bool]string{true: " (no cache)"}[noCache])
			return a.Engine.Rebuild(cmd.Context(), p, noCache)
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
		Long: `Removes the project's named volumes (databases, caches) and starts it
again from scratch. Host bind-mounts are NOT touched , only named volumes.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			if err := dockerx.EngineCheck(cmd.Context()); err != nil {
				return err
			}
			p, err := oneTarget(a, args)
			if err != nil {
				return err
			}
			if !force {
				vols, verr := a.Engine.Volumes(cmd.Context(), p)
				if verr != nil {
					fmt.Printf("Warning: could not list volumes for %s (%v) , reset will still run `down -v`.\n", p.Name, verr)
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
			return a.Engine.Reset(cmd.Context(), p)
		},
	}
	reset.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt")
	rootCmd.AddCommand(reset)
}
