# engine module

## Purpose

The model runtime port (Ollama first) and its fake for tests.

## Owns

- Packages: `internal/engine/` (the port, value types and typed errors), `internal/engine/ollama/` (the Ollama adapter, native API), `internal/engine/fake/` (deterministic fake for tests, not an adapter), `internal/engine/enginetest/` (the shared contract suite, req_001 D-31). Built by PLT-F-006.
- Tables: none.
- Port: `Engine`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-14, D-22, D-31, D-48. `on_context_overflow` = `refuse` (default) sends `truncate:false` and `shift:false` (chat) or `truncate:false` (embed) and maps Ollama's oversize error to `context_too_long`; `truncate` leaves Ollama's default. The adapter's minimum Ollama version and every timeout and limit are `ollama.Config` fields owned by the caller (D-38). The module map is in `docs/design_docs/lld.md` section 1.
