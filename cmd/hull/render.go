package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CavenRE/hull/internal/compose"
	"github.com/CavenRE/hull/internal/manifest"
)

// runRender implements `hull render [dir]`: manifest in, compose.yaml out.
// TLD and Hull home come from flags until the daemon owns global config
// (Phase 3).
func runRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	out := fs.String("o", "", "write to file instead of stdout (use 'compose.yaml' in the project)")
	tld := fs.String("tld", "test", "local TLD for generated domains")
	home := fs.String("hull-home", defaultHullHome(), "Hull home directory (xdebug.ini mount)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		return err
	}
	f, err := compose.Render(m, compose.Context{TLD: *tld, HullHome: *home})
	if err != nil {
		return err
	}
	data, err := compose.Marshal(f)
	if err != nil {
		return err
	}

	if *out == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return err
	}
	fmt.Println("Wrote", *out)
	return nil
}

func defaultHullHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".hull"
	}
	return filepath.ToSlash(filepath.Join(home, ".hull"))
}
