# settings module

## Purpose

Holds every runtime setting (time, retention, TTL, retry, threshold) with one owning party, so no value is hard-coded.

## Owns

- Packages: `internal/settings/` (built by PLT-F-004).
- Tables: `hst_*` (`hst_setting`), migrations in `internal/settings/migrations/`.
- Port: `settings owner model`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-38, D-70. The module map is in `docs/design_docs/lld.md` section 1.
