// Package dockerx wraps the Docker Engine API behind Hull's needs:
// socket discovery across engines (Docker Desktop, docker.io, Podman,
// OrbStack, Colima), health checks, container/compose lifecycle, and
// event streaming. (Phase 2)
package dockerx
