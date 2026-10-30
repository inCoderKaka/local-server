# ADR 0002: req_001 system design (HLD + LLD)

**Status:** Accepted
**Date:** 2026-10-30
**Extends:** [ADR 0001](0001-req001-scope-and-product-decisions.md)
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_001.md`
**Decisions:** D-21..D-33, D-38..D-45 in [`req_001.decisions.md`](../tracks/platform/phase_1/requirements/req_001.decisions.md). D-34..D-37 are superseded by D-41..D-44. The log is the source; this ADR summarises it.
**executive_inputs_reference:** execetive_inputs/26-10-30_platform_req_001_design-review.md, execetive_inputs/26-10-30_platform_req_001_design-sessions-verbatim.md (your exact prompts and answers, both design sessions)

## Context

ADR 0001 fixed scope, stack (Go, SQLite, a single binary), exposure, login and usage integrity. The design-review stage turned that into a system design (HLD) and a low-level design of state machines, call flows, data paths, relationships and schema (LLD). Midway through, the user's future-scoping input (`docs/reqs/phase1/req001_future_scoping.txt`) superseded four rows:
- D-35 → D-41: off-host data is decentralised by default.
- D-36 → D-43: sharing is generalised.
- D-34 → D-42: a unified principal.
- D-37 → D-44: short table prefixes, consent records and classification.

The full design, with every source inline, is in [`docs/architecture.md`](../architecture.md). The project has no AI logic of its own, so the AI-architecture stage was skipped (D-20).

## Decision

- **D-21 Process shape:** a host-only modular monolith. It has ports (engine, transport, storage, auth, notify, directory), modules (gateway, control plane, scheduler, ledger, notifier, host-metrics) and three listeners. The built-in UIs are API clients. CI enforces module boundaries.
- **D-22 Engine:** local-server owns an `Engine` port. The Ollama adapter uses the native API, always sets `num_ctx`, and meters from the native counts. local-server owns the model lifecycle and memory admission.
- **D-23 Tailscale:** system Tailscale by default, with embedded tsnet as an optional build tag.
- **D-24 Public mode:** Tailscale Funnel, opt-in and off by default. TLS ends on the host. Turning it on makes defence in depth mandatory. Cloudflare Tunnel plus our own end-to-end encryption stays a seam.
- **D-25 Identity:**
  - The host identity is an Ed25519 key.
  - Consumers are individual or application accounts with keys.
  - Each consumer has one grant, which a key can narrow.
  - IDs are UUIDv7. Keys are stored as a fast hash plus a prefix, and passwords as argon2id.
  - Keys can be bound to a Tailscale node through WhoIs.
  - Policy rules use our own versioned declarative format behind a `Policy` interface.
- **D-26 Scheduling:** two priority classes with aging, plus per-grant concurrency at or below `OLLAMA_NUM_PARALLEL`. Memory admission, and a bounded queue that fails fast with `Retry-After`. Async jobs with an `Idempotency-Key`. Results have a short TTL and are deleted on fetch.
- **D-27 Ledger:** an RFC 6962 Merkle log in SQLite via `x/mod/sumdb/tlog`. C2SP signed checkpoints. JWS `Ed25519` receipts in a header or the final SSE event. The ledger is written synchronously at request end, and rollups run async. The identity key signs a rotating receipt subkey.
- **D-28 Storage:**
  - `modernc.org/sqlite` (pure Go) in WAL mode, with one writer goroutine and a read pool.
  - Each module owns its tables behind its own repository.
  - Migrations are built in and run at startup.
  - A Postgres/hosted adapter is possible later.
- **D-29 Notifications:** a `Notifier` port with preferences. Channels: in-app over SSE, Standard Webhooks (HMAC `v1`) through a transactional outbox, and desktop via beeep in the user session only. Email and Telegram come later.
- **D-30 Packaging:**
  - A per-user login service by default, with an optional system service and a notification helper, via kardianos/service.
  - curl/PowerShell installers that verify SHA-256 and detect Ollama and Tailscale.
  - A `Power` sleep lock, held only while sharing.
- **D-31 Observability:** a `Telemetry` port (slog JSON plus Prometheus by default, OTel switchable). A `HostMetrics` port on gopsutil, with unattributed processes labelled. Contract tests for every adapter, golden tests, and CI on 3 OSes.
- **D-32 API:**
  - Spec-first OpenAPI 3.1. Standard surfaces are implemented as published and never modified. local-server features are additive contracts.
  - The admin API is on the admin listener only.
  - Errors use the OpenAI envelope plus typed codes. Unsupported fields are rejected.
  - Versions are major-in-URL and additive-only.
- **D-33 Headers:** extensions on OpenAI-compatible calls go in headers (`LS-Request-Properties`, `LS-Receipt`). When streaming, the receipt also comes in a final SSE event.
- **D-38 State machines:** for jobs, interactive requests, resource requests, keys, invites, consumers, host sharing, models, webhooks, sharing grants, signing keys and sync changes. Terminal states are never reopened. Every time, retention, TTL, retry and threshold is a runtime setting owned by one party, and lower parties may tighten only within the higher party's bounds.
- **D-39 Call flows:**
  - F1 interactive: auth → policy → validate → admit → engine → stream → one metering transaction → receipt.
  - If metering fails, the answer is still returned and the failure is alerted (user-confirmed).
  - F2 jobs, F3 resource requests, F4 webhooks, F5 checkpoints, F6 off-host, F7 sharing grants, F8 login, F9 startup.
- **D-40 Data paths:** content is never at rest except in two encrypted job tables, which are deleted at the terminal state or on fetch. Personal fields are erased on deletion while the row stays. Secret keys are sealed with an OS-keychain master key.
- **D-41 Off-host (supersedes D-35):** the default is host-encrypted snapshots to the host's own bucket (R2). Encrypted change-row sync to the platform admin's Supabase is opt-in per host. Also: restore, signed NDJSON export/import, migration traces and a recovery code.
- **D-42 Relationships (supersedes D-34):** D-34 stands, plus the unified `idn_principal`. Type tables reference it 1:1 and polymorphic actor pairs become `principal_id`. Sharing-grant and consent relationships are added.
- **D-43 Sharing (supersedes D-36):** a general sharing grant per dataset, with a purpose, terms version and expiry. The dataset key is wrapped to the grantee and rotated on expiry or revocation. Phase 1 implements only the platform admin as grantee, for opted-in hosts.
- **D-44 Schema (supersedes D-37):** D-37 stands, with short module prefixes (`hst_ idn_ grt_ req_ sch_ use_ ldg_ ntf_ aud_ syn_ plt_ ops_`), `idn_principal`, `plt_sharing_grant`, `idn_consent`, and a classification plus `billable` flag on every table and column in `ops_data_catalog`.
- **D-45 Future direction:** seams only:
  - categorisation and `plan_id`
  - consent and an off-by-default content-capture interface
  - benchmarking hooks
  - trace sharing
  - decentralised by default
  - legal decision traces (ADRs, executive inputs, `docs/legal/`)

## Alternatives considered

Only what the log records:
- **Processes:** separate services or multiple processes add install weight with no benefit at single-machine scale (D-21).
- **Engine:** the OpenAI-compatible engine API alone cannot set context size, so it can't enforce per-key caps (D-22).
- **Public option:** Cloudflare's HTTP proxy terminates TLS, and its TCP mode needs a client helper (D-24).
- **Ledger:**
  - A plain hash chain shows tampering only if someone audits it and keeps old heads (D-27).
  - Tessera needs other storage (D-27).
  - Trillian is in maintenance mode and needs MySQL (D-27).
- **Off-host:**
  - Litestream: no client-side encryption in v0.5, and Windows is unsupported. It's a possible later implementation, not chosen (D-41 via D-35).
  - A Supabase-by-default design was replaced by decentralised-by-default (D-41).
- **Policy:** CEL/Rego engines can replace the in-house format behind the `Policy` interface later (D-25). Their size cost is unsourced.

## Consequences

- **Transport:** two code paths (system Tailscale and tsnet) are accepted for flexibility. tsnet streaming must be re-checked before it's enabled.
- **Public mode:** it obliges rate limits, lockout and size/time limits on the consumer API.
- **Startup data:** a host signing key and the Merkle ledger are needed from day one. The receipt subkey limits a leaked key's damage.
- **Metering failure:** the alerted path means a rare answer can go out without a ledger entry. This is accepted for user experience.
- **Off-host data:** the default needs a host-owned bucket. Platform features need opt-in Supabase sync, which may need the Pro plan as hosts grow.
- **Schema:** the classification and billable metadata adds migration discipline. The generated schema doc must stay in sync.
- **To verify before ticketize (Unsourced):**
  - Tailscale Funnel on the Personal plan
  - Gatekeeper with an unsigned curl-downloaded binary
  - `OllamaSetup.exe` silent flags
  - whether Ollama silently truncates prompts beyond `num_ctx`
  - per-OS keychain support
- **Unsourced, lower priority:** Ed25519 webhook receiver support, OTel size, CEL/Rego size, and SDK handling of unknown body fields.

## Sources

Every source URL recorded on D-21..D-33 and D-38..D-45 is carried inline, next to its claim, in [`docs/architecture.md`](../architecture.md). Key ones:
- **Ollama:** [OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility), [api.md](https://raw.githubusercontent.com/ollama/ollama/main/docs/api.md), [FAQ](https://docs.ollama.com/faq)
- **Tailscale:** [Funnel](https://tailscale.com/kb/1223/funnel), [tsnet](https://tailscale.com/kb/1244/tsnet), [tsnet #21577](https://github.com/tailscale/tailscale/issues/21577), [pricing](https://tailscale.com/pricing)
- **Ledger and receipts:** [RFC 6962](https://www.rfc-editor.org/rfc/rfc6962.html), [C2SP tlog-checkpoint](https://c2sp.org/tlog-checkpoint), [x/mod/sumdb/tlog](https://pkg.go.dev/golang.org/x/mod/sumdb/tlog), [RFC 9864](https://www.rfc-editor.org/rfc/rfc9864.html), [Crosby & Wallach 2009](https://www.usenix.org/legacy/event/sec09/tech/full_papers/crosby.pdf)
- **Storage and services:** [modernc sqlite](https://pkg.go.dev/modernc.org/sqlite), [SQLite WAL](https://www.sqlite.org/wal.html), [SQLite VACUUM INTO](https://www.sqlite.org/lang_vacuum.html), [kardianos/service](https://pkg.go.dev/github.com/kardianos/service)
- **Webhooks:** [Standard Webhooks spec](https://raw.githubusercontent.com/standard-webhooks/standard-webhooks/main/spec/standard-webhooks.md)
- **Passwords:** [OWASP Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- **Off-host costs:** [Cloudflare R2 pricing](https://developers.cloudflare.com/r2/pricing/), [Supabase pricing](https://supabase.com/pricing)

Repo: `docs/reqs/phase1/req001_future_scoping.txt` (D-41..D-45).
