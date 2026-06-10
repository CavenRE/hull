// Package state maintains the project registry: a JSON index under the
// Hull home directory, rebuildable at any time by scanning registered
// roots for hull.yaml manifests. Files on disk are the truth; this is
// only an index. (Phase 2)
package state
