# telemetry module

## Purpose

Structured logs, metrics and request ids behind the Telemetry port. Prompts and responses are never logged.

## Owns

- Packages: `internal/telemetry/` (built by PLT-F-003).
- Tables: none.
- Port: `Telemetry`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-31, D-40. The module map is in `docs/design_docs/lld.md` section 1.
