// Command hulld is the Hull daemon: project engine, shared services,
// embedded Caddy router, wildcard DNS, and certificate trust behind a
// local socket API. The daemon proper arrives in Phase 3 of the roadmap.
package main

import (
	"fmt"

	"github.com/CavenRE/hull/internal/version"
)

func main() {
	fmt.Println("hulld", version.String(), "— daemon not yet implemented (Phase 3)")
}
