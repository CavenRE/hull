// Command hull is the Hull CLI — a thin client over the hulld local API.
// The full command set arrives in Phase 2; `render` exists now as the
// development face of the manifest → compose engine.
package main

import (
	"fmt"
	"os"

	"github.com/CavenRE/hull/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println("hull", version.String())
	case "render":
		err = runRender(os.Args[2:])
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "hull: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hull:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`Hull v2 — composable local development environment (under construction)

Usage: hull <command> [arguments]

Commands:
  render [dir]   Generate compose.yaml from a project's hull.yaml
  version        Print the Hull version
`)
}
