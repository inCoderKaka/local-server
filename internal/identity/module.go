package identity

import (
	"embed"
	"io/fs"

	"github.com/inCoderKaka/local-server/internal/storage"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Module registers the identity migrations with storage. Register it before any module whose
// tables reference idn_* (grants): storage applies modules in the order given.
func Module() storage.Module {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the embedded path is fixed at build time
	}
	return storage.Module{Name: "identity", Prefixes: []string{"idn_"}, Migrations: sub}
}
