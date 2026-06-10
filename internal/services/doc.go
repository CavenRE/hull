// Package services manages shared service instances (e.g. postgres-16,
// mariadb-lts, redis) that run globally with side-by-side versions, and
// the link/unlink operations that connect projects to them. Dedicated
// per-project services remain a manifest option. (Phase 5)
package services
