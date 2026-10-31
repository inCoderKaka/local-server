// Package storage is the storage module (docs/modules/storage.md): the only code that
// imports a database driver (D-09, D-28).
//
// # Model
//
// One SQLite file in WAL mode. Writes go through one writer goroutine that owns the only
// write connection; it groups queued units of work into one BEGIN IMMEDIATE transaction
// (a batch). Reads use a separate pool of query_only connections.
//
// # Unit of work
//
// DB.Write(ctx, fn) runs fn on the writer goroutine inside a savepoint of the current
// batch. Every row fn writes through the Tx (its own module's rows, audit, events,
// outbox, Tx.Capture) joins one atomic unit: if fn returns an error or panics, all of
// them roll back and the other units in the batch are unaffected; otherwise they commit
// with the batch and Write returns after the commit. If the batch itself fails (its
// commit, or a savepoint that cannot be rolled back or released), every unit in it that
// had not already failed returns an error and nothing of the batch is committed. fn must
// not call DB.Write or DB.Close (both would wait for the writer, which is running fn),
// must not keep the Tx, and should be short and free of network calls.
//
// # Migrations
//
// Each module owns a migrations/ folder of forward-only files NNNN_name.sql (NNNN is a
// number, strictly increasing within the module), embedded in the binary and registered
// with Config.Modules. Open applies the unapplied ones in order, each in its own
// transaction, and records them in ops_schema_migrations (with the SHA-256 checksum of
// the file) and ops_migration_run. A changed or missing applied file, or a new file that
// is older than an applied one, stops startup.
//
// # Catalog
//
// Migration files carry the plain-language comment, classification and billable flag of
// every table and column in "--@table" and "--@column" lines:
//
//	--@table  idn_consumer classification=personal billable=true | Comment text.
//	--@column idn_consumer.email classification=personal billable=false | Comment text.
//
// Open collects them into ops_data_catalog and fails if a table or column has none, or
// if a table does not start with one of its module's prefixes (D-44). To correct the
// catalog line of an applied migration, or add a missing one, add a new migration that
// carries only the corrected "--@" lines; a later migration's line overrides an earlier one.
//
// # Configuration
//
// Every duration and limit is a Config field with a documented default. Storage
// cannot import the settings module (PLT-F-004), so startup reads the settings and
// passes them in.
//
// Prompts and responses are never written by this package (D-31, D-40).
package storage

//go:generate go test -run TestSchemaDoc -update .
