package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CavenRE/hull/internal/api"
	"github.com/CavenRE/hull/internal/engine"
)

func init() {
	var (
		php    string
		domain string
		serve  bool
	)
	cmd := &cobra.Command{
		Use:   "set <project>",
		Short: "Change a managed project's settings",
		Long: `Update a project's PHP version, domain, or serve flag. Only the
flags you pass are changed. Applies through a running daemon when one is up.`,
		Example: `  hull set myapp --php 8.3
  hull set myapp --domain shop
  hull set worker --serve=false`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			name := args[0]

			var req api.PatchProjectRequest
			var opts engine.PatchOptions
			if cmd.Flags().Changed("php") {
				req.PHP, opts.PHP = &php, &php
			}
			if cmd.Flags().Changed("domain") {
				req.Domain, opts.Domain = &domain, &domain
			}
			if cmd.Flags().Changed("serve") {
				req.Serve, opts.Serve = &serve, &serve
			}
			if req.PHP == nil && req.Domain == nil && req.Serve == nil {
				return fmt.Errorf("nothing to change — pass --php, --domain, or --serve")
			}

			if client, ok := a.client(); ok {
				if err := client.PatchProject(cmd.Context(), name, req); err != nil {
					return err
				}
			} else {
				p, err := a.findProject(name)
				if err != nil {
					return err
				}
				if err := a.Engine.SetProjectFields(p, opts); err != nil {
					return err
				}
			}
			fmt.Printf("✔ %s updated\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&php, "php", "", "PHP version (e.g. 8.3)")
	cmd.Flags().StringVar(&domain, "domain", "", "local domain label")
	cmd.Flags().BoolVar(&serve, "serve", true, "whether the project gets a routed domain")
	rootCmd.AddCommand(cmd)
}
