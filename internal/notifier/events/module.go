package events

import (
	"embed"
	"io/fs"

	"github.com/inCoderKaka/local-server/internal/storage"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Module registers the events migrations with storage.
func Module() storage.Module {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the embedded path is fixed at build time
	}
	return storage.Module{Name: "events", Prefixes: []string{"ntf_"}, Migrations: sub}
}
