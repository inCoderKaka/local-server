# gateway module

## Purpose

The consumer API: OpenAI-compatible endpoints and the request pipeline.

## Owns

- Packages: `internal/gateway/` (built by PLT-F-018, PLT-F-019).
- Tables: none.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-21, D-39. The module map is in `docs/design_docs/lld.md` section 1.
