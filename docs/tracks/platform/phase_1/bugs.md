# PLT track — bugs (phase_1)

Bug registry for the platform track. Convention: `docs/tracks/README.md`.
Statuses: `reported → confirmed → fixed → shipped`; `dropped` (reason). Default P2.

| ID | Pri | Status | Description / Ref | Source |
|---|---|---|---|---|
| PLT-B-001 | P2 | reported | CI actions in `.github/workflows/ci.yml` and `fuzz.yml` use floating `@v7` tags, no SHA pins, no update bot; a moved tag runs unreviewed code, and PLT-C-003's release job will reuse them. Needs a D-row next to D-62: (a) SHA-pin + dependabot (recommended, before PLT-C-003), (b) pin by hand, (c) keep floating. Gate set A `7454922..3cf4f3f` finding 9; found in PLT-C-001. req_001 D-62 · P1/B1/S1 · verify: `grep -nE "uses: .*@v[0-9]+$" .github/workflows/*.yml` prints nothing | review-gate |
| PLT-B-002 | P2 | reported | archcheck does not guard the telemetry adapter: no `Adapters` entry for `internal/telemetry/slogprom`, no `Restricted` entry for `prometheus/client_golang`, so any module can import them. Found in PLT-F-003 (`ef86df3`). req_001 D-21, D-31 · P1/B1/S4 · Model: Sonnet · verify: a planted import of `internal/telemetry/slogprom` from another module makes `go run ./tools/archcheck` exit 1 | build |
| PLT-B-003 | P2 | reported | ogen's default error handler writes `{"error_message": ...}`, not the OpenAI error envelope, and decode errors can quote request bytes; PLT-F-018 and PLT-F-024 must install `WithErrorHandler`, `WithNotFound`, `WithMethodNotAllowed` and never log decode text verbatim. Found in PLT-F-001 (`f00fa8a`). req_001 D-31, D-33, D-40 · P1/B3/S19 · Model: Sonnet · verify: `go test ./internal/gateway/ -run ErrorEnvelope` | build |
| PLT-B-004 | P2 | reported | Generated SSE response path copies the stream without flushing; PLT-F-019 needs a writer that flushes after each write. Found in PLT-F-001 (`f00fa8a`). req_001 D-33 · P1/B3/S20 · Model: Sonnet · verify: `go test ./internal/gateway/ -run StreamFlush` | build |
| PLT-B-005 | P1 | reported | One ogen `Server` carries both `/v1` and `/admin/v1`; each listener must mount only its own prefix, or admin routes reach the consumer listener. Found in PLT-F-001 (`f00fa8a`). req_001 D-10 · P1/B4/S25 · Model: Sonnet · verify: `go test ./internal/gateway/ -run ListenerPrefix` (admin path on consumer listener → 404) | build |
| PLT-B-006 | P3 | reported | Upstream: ogen v1.24.0 panics with a nil pointer on any `x-ogen-type` extension; the spec avoids it. Found in PLT-F-001 (`f00fa8a`). | build |
