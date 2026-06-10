// Package version holds Hull's build-time version metadata.
package version

// Overridden at release time via:
//
//	-ldflags "-X github.com/CavenRE/hull/internal/version.Version=... -X github.com/CavenRE/hull/internal/version.Commit=..."
var (
	Version = "dev"
	Commit  = "none"
)

// String returns the human-readable version, e.g. "dev (none)".
func String() string {
	return Version + " (" + Commit + ")"
}
