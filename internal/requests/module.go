package requests

import (
	"embed"
	"io/fs"

	"github.com/inCoderKaka/local-server/internal/storage"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Module registers the requests migrations with storage. Its tables reference idn_* and grt_*
// tables, and storage applies modules in the order given, so register identity.Module() and
// grants.Module() first.
func Module() storage.Module {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the embedded path is fixed at build time
	}
	return storage.Module{Name: "requests", Prefixes: []string{"req_"}, Migrations: sub}
}
