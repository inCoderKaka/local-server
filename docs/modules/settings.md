# settings module

## Purpose

Holds every runtime setting (time, retention, TTL, retry, threshold) with one owning party, so no value is hard-coded.

## Owns

- Packages: `internal/settings/` (built by PLT-F-004): `Store` (register, get, set, reset), the retention registry and `Run`/`PruneOnce` worker, and `Module()` for storage.
- Tables: `hst_*` (`hst_setting`), migrations in `internal/settings/migrations/`.
- Port: `settings owner model`: modules register a `Def` (key, type, default, owner, description, optional scope); only the owning party (platform admin, host or consumer) may `Set` it, and no other party limits the value to a range (D-54); the only other refusal is a write validator the owning module registers with `SetValidator` or `SetTxValidator` (`ErrRejected`). Retention rules (`Rule`: table, time column, duration setting, optional condition) are registered with `RegisterRetention`; one worker deletes expired rows through the storage unit of work, and its interval (`retention.interval`) and batch size (`retention.batch_rows`) are host-owned settings. A retention of zero or less keeps rows forever.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-38, D-54, D-40, D-70. The module map is in `docs/design_docs/lld.md` section 1.
