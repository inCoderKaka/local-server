# engine module

## Purpose

The model runtime port (Ollama first) and its fake for tests.

## Owns

- Packages: `internal/engine/` (built by PLT-F-006).
- Tables: none.
- Port: `Engine`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-14, D-22. The module map is in `docs/design_docs/lld.md` section 1.
