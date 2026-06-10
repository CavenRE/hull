// Package templates defines Hull's built-in project templates (laravel,
// wordpress, plain) and service engines (postgres, mysql, mariadb, redis)
// as data consumed by the compose renderer — ported from v1's
// templates/*.yaml, replacing yq merging. Scaffold hook assets get
// go:embed-ed here in Phase 2; user template directories layer on top
// later. (Phase 1)
package templates
