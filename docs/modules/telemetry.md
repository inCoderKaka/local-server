# telemetry module

## Purpose

Structured logs, metrics and request ids behind the Telemetry port (structured log, counter, gauge, histogram, span). Prompts and responses are never logged: content is wrapped in the `Redacted` type, which always logs as `[redacted]`, and metric labels named like content are refused (D-31, D-40).

## Owns

- Packages: `internal/telemetry/` (the port, `Redacted`, request id context helpers and HTTP middleware, `Nop`) and `internal/telemetry/slogprom/` (the default adapter: slog JSON, Prometheus registry served as an `http.Handler` for `/metrics`, no-op spans, rotating log file through lumberjack). Built by PLT-F-003.
- Tables: none.
- Port: `Telemetry`.

## Imports

The port package imports only the standard library. `slogprom` is the only importer of `gopkg.in/natefinch/lumberjack.v2` (D-58) and the Prometheus client. Log rotation size and age are `slogprom.FileConfig` fields (defaults 50 MB and 14 days); startup wires them from the log-retention settings once the settings module (PLT-F-004) exists. Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-31, D-40, D-58. The module map is in `docs/design_docs/lld.md` section 1.
