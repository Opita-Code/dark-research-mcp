// Package mem persists dark-research runs and items to SQLite so the
// agent can recall past research, cross-link with dark-eval attacks,
// and survive process restarts.
//
// dark-mem is its own package, separate from dark-eval's mem
// (internal/dark/mem in the crush fork). The two share the same SQLite
// file by convention, so cross-table queries are possible via direct
// SQL when needed, but the API surfaces stay independent.
//
// Schema is versioned via AllMigrations (see migrate.go). On Open(), the
// store runs every pending migration and records it in schema_migrations.
package mem