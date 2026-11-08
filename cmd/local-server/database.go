package main

import (
	"context"
	"fmt"

	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// database is the open database with its settings store and the synchronous mode it was opened
// with.
type database struct {
	db       *storage.DB
	st       *settings.Store
	syncMode string
}

// openDatabase opens the database with every registry module (migrations), registers the
// settings, and applies storage.synchronous (req_001 D-39 F9 steps 2 and 3, D-76). The setting
// lives in hst_setting, so it cannot be read before the first open: the database opens with the
// default (FULL, also the first-run value), the setting is read through the store, and when it
// differs the database is closed and opened again with Config.Synchronous set to it.
func openDatabase(ctx context.Context, path string) (*database, error) {
	d, err := openWith(ctx, path, "")
	if err != nil {
		return nil, err
	}
	raw, err := d.st.String(ctx, storage.KeySynchronous, "")
	if err != nil {
		d.db.Close()
		return nil, fmt.Errorf("read setting %s: %w", storage.KeySynchronous, err)
	}
	want, err := storage.ParseSynchronous(raw)
	if err != nil {
		d.db.Close()
		return nil, fmt.Errorf("setting %s: %w", storage.KeySynchronous, err)
	}
	if want == d.syncMode {
		return d, nil
	}
	if err := d.db.Close(); err != nil {
		return nil, err
	}
	return openWith(ctx, path, want)
}

func openWith(ctx context.Context, path, mode string) (*database, error) {
	cfg := storage.DefaultConfig(path, registryModules()...)
	cfg.Synchronous = mode
	db, err := storage.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	st := settings.New(db)
	if err := registerSettings(st); err != nil {
		db.Close()
		return nil, err
	}
	// One minimize switch per table of ops_data_catalog, which the open above filled (req_001 D-64).
	if err := snapshot.RegisterSettings(ctx, db, st); err != nil {
		db.Close()
		return nil, err
	}
	used, err := storage.ParseSynchronous(mode)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &database{db: db, st: st, syncMode: used}, nil
}
