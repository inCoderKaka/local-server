package settings

import "embed"

//go:embed migrations/*.sql
var ownMigrations embed.FS
