package audit

import (
	"embed"
	"io/fs"

	"github.com/inCoderKaka/local-server/internal/storage"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Module registers the audit migrations with storage. aud_event references idn_principal, so
// register the identity module before this one: storage applies modules in the order given.
func Module() storage.Module {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the embedded path is fixed at build time
	}
	return storage.Module{Name: "audit", Prefixes: []string{"aud_"}, Migrations: sub}
}
