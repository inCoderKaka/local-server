package usage

import (
	"embed"
	"io/fs"

	"github.com/inCoderKaka/local-server/internal/storage"
)

//go:embed migrations/*.sql
var ownMigrations embed.FS

// Module returns the storage registration of the usage migrations (prefix use_).
func Module() storage.Module {
	sub, err := fs.Sub(ownMigrations, "migrations")
	if err != nil {
		panic(err) // the embedded directory is fixed at build time
	}
	return storage.Module{Name: "usage", Prefixes: []string{"use_"}, Migrations: sub}
}
