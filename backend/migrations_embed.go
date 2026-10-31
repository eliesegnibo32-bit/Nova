// Package novaapi is the root package of the nova-api module. It exists only
// to expose the embedded SQL migrations (located at ./migrations/*.sql) to the
// rest of the codebase. The embed directive cannot walk up directories
// (no ".." allowed), so this file lives at the module root alongside the
// migrations/ folder and exports the embed.FS for internal/db to consume.
package novaapi

import "embed"

// MigrationsFS embeds every goose SQL migration file under ./migrations.
// It is consumed by internal/db.RunMigrations via goose.SetBaseFS.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
