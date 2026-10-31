package host

import (
	"embed"
	"io/fs"

	"github.com/inCoderKaka/local-server/internal/storage"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Module registers the host module's migrations with storage (tables hst_host and hst_key,
// created for the key custody in internal/hostkeys, and hst_metric_minute, written by
// internal/hostmetrics). The settings module registers its own
// hst_setting table under the same prefix; the two do not overlap.
func Module() storage.Module {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the embedded path is fixed at build time
	}
	return storage.Module{Name: "host", Prefixes: []string{"hst_"}, Migrations: sub}
}
