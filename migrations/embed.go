// Package migrations embeds the plain-SQL migration files so that binaries
// (including the Docker image) carry their own schema history.
package migrations

import "embed"

// FS holds every *.sql migration in this directory, applied in filename order
// by goose in cmd/migrate.
//
//go:embed *.sql
var FS embed.FS
