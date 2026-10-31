package usage

import (
	"context"
	"embed"
	"errors"
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

// ErrNotFound is returned by ConsumerOf for a usage id with no record.
var ErrNotFound = errors.New("usage: no usage record with this id")

// ConsumerOf returns the consumer that owns the usage record id, or ErrNotFound. The
// gateway uses it so a consumer can fetch the inclusion proof of its own records only.
func ConsumerOf(ctx context.Context, rd *storage.Reader, id string) (string, error) {
	rows, err := rd.Query(ctx, `SELECT consumer_id FROM use_record WHERE id = ?`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", ErrNotFound
	}
	var consumer string
	if err := rows.Scan(&consumer); err != nil {
		return "", err
	}
	return consumer, nil
}
