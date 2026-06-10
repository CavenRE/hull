// Package router manages the embedded Caddy instance: HTTPS routes are
// derived from the project registry and updated live on project state
// changes. Replaces v1's caddy-docker-proxy container. (Phase 4; v1
// router is used as-is through Phases 2-3)
package router
