# engine module

## Purpose

The model runtime port (Ollama first) and its fake for tests.

## Owns

- Packages: `internal/engine/` (the port, value types and typed errors), `internal/engine/ollama/` (the Ollama adapter, native API), `internal/engine/fake/` (deterministic fake for tests, not an adapter), `internal/engine/enginetest/` (the shared contract suite, req_001 D-31). Built by PLT-F-006.
- Tables: none.
- Port: `Engine`. `ChatRequest` carries optional `Temperature`, `TopP`, `Seed` and `Stop`, sent to Ollama as `options` only when set (PLT-F-059); the gateway still refuses them (PLT-B-031). It also carries optional per-call `FirstTokenTimeout` and `IdleTimeout` for a streamed answer; zero or less means the adapter's own fallback, and the gateway fills both from its `gateway.stream_*` and `gateway.engine_timeout` settings (PLT-B-039). The Ollama live test (`-tags ollama`, `LS_OLLAMA_URL`) takes model names from `LS_OLLAMA_CHAT_MODEL` and `LS_OLLAMA_EMBED_MODEL` (PLT-F-058).

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-14, D-22, D-31, D-48. `on_context_overflow` = `refuse` (default) sends `truncate:false` and `shift:false` (chat) or `truncate:false` (embed) and maps Ollama's oversize error to `context_too_long`; `truncate` leaves Ollama's default. The adapter's minimum Ollama version and every timeout and limit are `ollama.Config` fields owned by the caller (D-38). The module map is in `docs/design_docs/lld.md` section 1.
