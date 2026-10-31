package ledger

import "embed"

//go:embed migrations/*.sql
var ownMigrations embed.FS
