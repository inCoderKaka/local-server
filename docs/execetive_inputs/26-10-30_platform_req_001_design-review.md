> **Provenance note.** These questions and answers are reconstructed from `docs/tracks/platform/phase_1/requirements/req_001.decisions.md` (the `HLD:` and `LLD:` rows D-21..D-33 and D-38..D-45, plus the superseded D-34..D-37) on 2026-10-30, at the design-close of the design-review stage. They are verbatim only for what the user said in a chat. Answered `Q-NN` rows were deleted from the log when they were decided, so the original question text did not survive. Where no question text exists, it says "question text not recorded". The user's own words quoted inside the decisions ("OpenAPI and other standards compatible + additional contracts defined specifically by this app"; "data should be decentralized as much as possible unless the user decides to go for centralized data"; "plt_ instead of platform_"; "experience first" on the metering-failure path; the switchable-telemetry ask; the everything-configurable ask) are the user's answers as the log quotes them. The future-scoping input is in `docs/reqs/phase1/req001_future_scoping.txt`.

agent questions:
- Q-16 (HLD: Tailscale integration and a no-plaintext public option): question text not recorded.
- Q-17 (HLD: process shape): question text not recorded.
- Q-18 (HLD: engine port): question text not recorded.
- Q-19 (HLD: identity and authorization): question text not recorded.
- Q-20 (HLD: admission and scheduling): question text not recorded.
- Q-21 (HLD: usage ledger and receipts): question text not recorded.
- Q-22 (HLD: storage): question text not recorded.
- Q-23 (HLD: notifications): question text not recorded.
- Q-24 (HLD: packaging and OS integration): question text not recorded.
- Q-25 (HLD: observability and maintainability): question text not recorded.
- Q-26 (HLD: API contract and extension placement): question text not recorded.
- Q-27 (LLD: state machines): question text not recorded.
- Q-28 (LLD: call flows): question text not recorded.
- Q-29 (LLD: data paths): question text not recorded.
- Q-30 (LLD: schema): question text not recorded.
- Q-31 (LLD: relationships): question text not recorded.
- Q-32 (HLD: off-host durability and platform admin role, raised by the user): question text not recorded.
- Future-scoping input (user-supplied file, not a question): its effect on D-34..D-37.

user response:
(Decisions as recorded in the log; the log is the source of truth. Full text with sources is in the log and in docs/architecture.md.)
- D-21 (answers Q-17): Host-only modular monolith: one binary, ports and adapters; modules gateway, control plane, scheduler, ledger, notifier, host-metrics; adapters engine, transport, storage, auth, notify, directory; three listeners; built-in SPAs calling the same versioned HTTP API; module boundaries enforced in CI; `directory` port no-op in Phase 1.
- D-22 (answers Q-18): local-server owns an `Engine` interface; Ollama adapter uses the native API; always sets `num_ctx`; owns model lifecycle and memory admission; metering from native counts; llama.cpp and MLX later.
- D-23 (answers Q-16, with D-24): Tailscale both ways behind the transport adapter: system Tailscale default; embedded tsnet optional build tag (streaming-stall issue re-checked first).
- D-24 (answers Q-16): Tailscale Funnel as an opt-in public mode, off by default, in Phase 1; defence in depth mandatory when enabled; Cloudflare Tunnel + own E2E stays a future seam. Unsourced: Funnel on the Personal plan.
- D-25 (answers Q-19): Ed25519 host identity key; consumer accounts (individual | application) with keys; grant per consumer, key can narrow; UUIDv7 IDs; keys shown once, fast hash + prefix; argon2id passwords; optional WhoIs node binding; own versioned declarative policy format behind a `Policy` interface.
- D-26 (answers Q-20): In-process scheduler, interactive ahead of background with aging; per-grant concurrency ≤ `OLLAMA_NUM_PARALLEL`; memory admission; bounded queue failing fast with 429/503 + Retry-After; async job API with Idempotency-Key; job results short TTL, deleted on fetch.
- D-27 (answers Q-21): Merkle-tree ledger in SQLite (RFC 6962 via x/mod/sumdb/tlog), C2SP signed checkpoints; JWS Ed25519 receipts in a header or final SSE event; synchronous append at request end, async rollups; rotating receipt subkey signed by the identity key; keys at `/v1/keys`.
- D-28 (answers Q-22): modernc.org/sqlite (pure Go), WAL, single writer goroutine with BEGIN IMMEDIATE, read pool, busy_timeout; module-owned repositories; built-in migrations; scheduled backups; Postgres adapter later.
- D-29 (answers Q-23): `Notifier` interface with per-user preferences; in-app (SQLite + SSE), Standard Webhooks via a transactional outbox, desktop via beeep in the user session; email and Telegram later.
- D-30 (answers Q-24): Per-user login service by default (LaunchAgent, systemd --user, Windows logon task), optional system service with a session helper, via kardianos/service; curl/PowerShell installers verifying SHA-256, detecting Ollama and Tailscale; `Power` sleep lock while sharing. Unsourced: Gatekeeper with unsigned binary; OllamaSetup.exe silent flags.
- D-31 (answers Q-25): `Telemetry` adapter, default slog JSON + Prometheus `/metrics`, OTel switchable ("the user asked for this to be switchable"); never log prompts/responses; `HostMetrics` on gopsutil with unattributed processes; CI module boundaries, contract tests per adapter, golden tests, CI on 3 OSes.
- D-32 (answers Q-26, with D-33): Spec-first OpenAPI 3.1; "OpenAPI and other standards compatible + additional contracts defined specifically by this app"; standard surfaces never modified; additive contracts; admin API on admin listener only; OpenAI error envelope + typed codes; unsupported fields rejected; additive-only versioning with Deprecation/Sunset.
- D-33 (answers Q-26): Extensions in headers (`LS-Request-Properties`, `LS-Receipt`); final SSE event for streaming.
- D-34 (answers Q-31): relationships — superseded by D-42.
- D-35 (answers Q-32, with D-36): off-host durability with Supabase default — superseded by D-41.
- D-36 (answers Q-32): platform admin approval per dataset, time-boxed — superseded by D-43.
- D-37 (answers Q-30): schema with long module prefixes — superseded by D-44.
- D-38 (answers Q-27): State machines for job, interactive request, resource request, access key, invite, consumer, host sharing, model, webhook delivery/endpoint, platform-admin approval, signing key, sync change. Configurability (user request): every time, retention, TTL, retry and threshold value is a stored setting editable at runtime by its owning party, never hard-coded; lower parties tighten within higher parties' bounds (interpreted; correct it if wrong).
- D-39 (answers Q-28): Call flows F1–F9. On persistent metering-write failure the answer is still returned and the failure logged and alerted (user confirmed: experience first). Unsourced: whether Ollama truncates prompts beyond `num_ctx` silently.
- D-40 (answers Q-29): Data paths: content never at rest except two encrypted job tables deleted on terminal state / fetch; consumer personal fields erased on deletion while the row stays; secrets sealed by a host master key in the OS keychain else a user-only file. Unsourced: per-OS keychain API support.
- D-41 (supersedes D-35): Off-host decentralized by default ("data should be decentralized as much as possible unless the user decides to go for centralized data"): host-encrypted snapshots to the host's own S3-compatible bucket (R2); Supabase platform sync opt-in per host.
- D-42 (supersedes D-34): Unified `idn_principal` (user's design) with per-type tables linked 1:1; polymorphic pairs become `principal_id`; sharing-grant and consent relationships.
- D-43 (supersedes D-36): Generalized sharing grant (any grantor → grantee, per dataset, purpose, terms version, expiry, key wrapping); Phase 1 implements only the platform admin as grantee for opted-in hosts.
- D-44 (supersedes D-37): Short module prefixes ("plt_ instead of platform_"; interpreted as all modules); `idn_principal`, `plt_sharing_grant`, `idn_consent`; classification + billable metadata in `ops_data_catalog`.
- D-45: Future-scoping direction recorded as seams only: categorization, consent and an off-by-default content-capture interface, benchmarking and smart host selection, trace sharing, decentralized by default, legal decision traces (ADRs, executive inputs, docs/legal/); only the current phase's legal needs are discussed.
