package scheduler

import (
	"embed"
	"io/fs"

	"github.com/inCoderKaka/local-server/internal/storage"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Module returns the storage registration of the scheduler module's migrations (prefix sch_).
// The tables are the background jobs of internal/jobs (req_001 D-26, D-40). The scheduler's own
// queue stays in memory.
func Module() storage.Module {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		panic(err) // the embedded path is fixed at build time
	}
	return storage.Module{Name: "scheduler", Prefixes: []string{"sch_"}, Migrations: sub}
}
