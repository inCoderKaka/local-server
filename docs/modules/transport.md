# transport module

## Purpose

How consumers reach the host: system Tailscale, Funnel, embedded tsnet.

## Owns

- Packages: `internal/transport/` (designed: PLT-F-038..F-040; not built).
- Tables: none.
- Port: `transport`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-12, D-23, D-24. The module map is in `docs/design_docs/lld.md` section 1.
