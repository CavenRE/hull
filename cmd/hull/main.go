// Command hull is the Hull CLI — a thin client over the hulld local API.
// Real commands arrive in Phase 2 of the roadmap.
package main

import (
	"fmt"
	"os"

	"github.com/CavenRE/hull/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("hull", version.String())
		return
	}
	fmt.Println("Hull v2 is under construction. Try: hull --version")
}
