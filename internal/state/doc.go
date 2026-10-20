// Package state finds projects on disk: it scans the configured roots, and the
// projects registered outside them, for hull.yaml files, legacy compose
// folders and plain folders. Nothing is cached; every call reads the folders
// again.
package state
