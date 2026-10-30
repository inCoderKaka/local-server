# local-server architecture

> **Status: planned.** None of this is built yet (F-03: the repo has no code). Every section describes the decided design for platform phase_1 req_001 and cites [ADR 0002](adr/0002-req001-system-design.md). The decision rows (`D-NN`) live in [`req_001.decisions.md`](tracks/platform/phase_1/requirements/req_001.decisions.md), which is the source of truth. Scope and product decisions are in [ADR 0001](adr/0001-req001-scope-and-product-decisions.md), and terms in [`glossary.md`](glossary.md).
>
> Source labels: a link backs the claim next to it. **Judgement** marks a reasoned call with no external source. **Unsourced** marks a claim to verify before ticketize or implementation. **Repo** points to a file in this or a sibling repo.

## Overview

| Concern | Decision |
|---|---|
| Process shape | One local-server binary per host. It is a modular monolith with ports and adapters (D-21) |
| Engine | Ollama through its native API, behind an `Engine` port (D-22) |
| Transport | System Tailscale by default, embedded tsnet as an optional build, and Funnel as an opt-in public mode (D-23, D-24) |
| Identity | An Ed25519 host identity, consumer accounts and keys, grants, and a unified principal (D-25, D-42) |
| Scheduling | An in-process scheduler with two priority classes and memory admission, plus an async job API (D-26) |
| Usage integrity | A Merkle-tree ledger with C2SP checkpoints and JWS receipts (D-27) |
| Storage | SQLite (pure Go) with a single writer, module-owned tables and short prefixes (D-28, D-44) |
| Off-host backups | Decentralised by default: encrypted snapshots to the host's own bucket. Platform sync is opt-in (D-41) |
| Data sharing | Sharing grants per dataset. Phase 1 grantee: the platform admin only (D-43) |
| Notifications | In-app over SSE, Standard Webhooks via an outbox, and desktop (D-29) |
| Packaging | A per-user login service by default, curl/PowerShell installers, and a sleep lock while sharing (D-30) |
| Observability | A `Telemetry` port with slog and Prometheus by default, plus a `HostMetrics` port (D-31) |
| API | Spec-first OpenAPI 3.1: standard surfaces unchanged, local-server features as additive contracts (D-32, D-33) |
| Configurability | Every time, retention, TTL, retry and threshold is a stored setting owned by one party (D-38) |
| Future direction | Seams only: categorisation, consent, benchmarking, trace sharing, legal traces (D-45) |

## Process shape and modules (D-21)

One binary on the host. Consumers install nothing from local-server: they use a client app (e.g. the PA adapter, ADR 0001 D-03) or a browser.

- **Internal modules:** gateway, control plane (accounts, keys, grants, requests), scheduler, ledger, notifier, host-metrics.
- **Ports, each with its adapters:** `Engine`, transport, storage, auth, notify, `directory`. The `directory` port is a no-op in Phase 1 and is the seam for a future central server or marketplace.
- **Listeners:** consumer API, consumer web page, admin UI. Both UIs are single-page apps built into the binary. They call the same versioned HTTP API, so a hosted website can replace them later.
- **Boundaries:** CI enforces module boundaries with forbidden-import checks. Optional modules (desktop notifier, embedded Tailscale) can be compiled out with build tags or start lazily.
- **Why:** lightest footprint and one install. Separate services would add install weight with no benefit at single-machine scale. Binary-size evidence: tsnet adds about 25 MB (34.2 MB with it vs 8.7 MB without, [gauger #11](https://github.com/mach4-braai/gauger/issues/11)).
- **Judgement:** per-request panic recovery contains the blast radius, and the UI has no in-process privileges.

## Engine (D-22)

- local-server owns an `Engine` interface: `Chat`, `Embed`, `ListModels`, `ModelInfo`, `Load`, `Unload`, `Loaded`.
- The Ollama adapter uses the native API: `/api/chat` and `/api/embed` with `options.num_ctx` and `keep_alive`, plus `/api/ps` and `/api/show` ([api.md](https://raw.githubusercontent.com/ollama/ollama/main/docs/api.md), [embed](https://docs.ollama.com/api/embed), [ps](https://docs.ollama.com/api/ps)).
- The gateway translates the consumer-facing OpenAI-compatible contract into native calls. The native API is needed because the OpenAI-compatible layer cannot set context size ([OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility), [context length](https://docs.ollama.com/context-length)).
- local-server owns the model lifecycle (preload, keep-alive, unload) and memory admission. It always sets `num_ctx` explicitly ([FAQ](https://docs.ollama.com/faq)).
- Metering reads `prompt_eval_count` and `eval_count` ([usage](https://docs.ollama.com/api/usage)).
- llama.cpp ([server README](https://raw.githubusercontent.com/ggml-org/llama.cpp/master/tools/server/README.md)) and MLX ([mlx-lm SERVER.md](https://raw.githubusercontent.com/ml-explore/mlx-lm/main/mlx_lm/SERVER.md)) come later as adapters.
- **Judgement:** this is an anti-corruption layer, and the translation code is small.

## Transport and exposure (D-10, D-23, D-24)

- **System Tailscale (default):** the host runs the normal Tailscale app, and the installer detects it and offers to install it. local-server listens on the tailnet and uses WhoIs ([client/local](https://pkg.go.dev/tailscale.com/client/local)) and `*.ts.net` HTTPS certificates ([HTTPS](https://tailscale.com/kb/1153/enabling-https)).
- **Embedded tsnet (optional build tag):** a single-install host ([tsnet KB](https://tailscale.com/kb/1244/tsnet), [pkg](https://pkg.go.dev/tailscale.com/tsnet)). Before tsnet is enabled for streamed responses, its streaming-stall issue must be re-checked ([#21577](https://github.com/tailscale/tailscale/issues/21577)).
- **Public mode (opt-in, off by default):** Tailscale Funnel. TLS terminates on the host, and the relays do not decrypt ([Funnel](https://tailscale.com/kb/1223/funnel)). Consumers such as PA on Fly then need no Tailscale.
  - Turning it on makes these mandatory on the consumer API: auth, rate limits, brute-force lockout, and request size and time limits.
  - Funnel limits: ports 443/8443/10000, bandwidth that can't be configured, `*.ts.net` names only.
  - Funnel is "available for all plans", including the free Personal plan, but it is **in beta**. The Personal plan is non-commercial only (F-06, [Funnel docs](https://tailscale.com/docs/features/tailscale-funnel.md), [pricing](https://tailscale.com/pricing)).
- **Caveats:**
  - Tagged nodes from another tailnet cannot reach shared machines ([tags](https://tailscale.com/kb/1068/tags), [sharing](https://tailscale.com/kb/1084/sharing), [Fly.io](https://tailscale.com/kb/1132/flyio)). So PA on Fly must be a user-owned node, or the consumer must be invited as a user (6 users on the free plan, non-commercial, [pricing](https://tailscale.com/pricing)).
  - `*.ts.net` names appear in public Certificate Transparency logs. ([Tailscale HTTPS](https://tailscale.com/kb/1153/enabling-https))
  - The passkey RP ID is bound to the host's hostname ([WebAuthn L3](https://www.w3.org/TR/webauthn-3/)).
- **Admin listener:** localhost by default. A setting adds a listener on the host's tailnet address behind an admin login. It is never public.
- **Rejected:** Cloudflare's HTTP proxy, because it terminates TLS ([DNS proxy](https://developers.cloudflare.com/dns/proxy-status/use-cases/)), and Cloudflare's TCP mode, because it needs a client helper ([arbitrary TCP](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/non-http/cloudflared-authentication/arbitrary-tcp/), [Spectrum](https://developers.cloudflare.com/spectrum/reference/configuration-options/)). Cloudflare Tunnel plus our own end-to-end encryption stays a seam behind the transport adapter.

## Identity and authorization (D-25, D-42)

- **Host identity:** each install generates an Ed25519 key pair, and its public key is the host's global ID (the marketplace seam).
- **Principal:** `idn_principal` holds what every actor shares: id, kind (`host_admin` | `consumer` | `platform_admin` | future), category/segment tags, a `plan_id` placeholder, consent links, created_at and status. Each type table (`idn_admin_user`, `idn_consumer`, `plt_admin`) has only its own fields and references the principal 1:1. Every polymorphic actor reference becomes one `principal_id`.
- **Consumers:**
  - A consumer is of kind `individual` or `application` and has zero or more access keys. A key-only consumer has no login credentials.
  - A grant per consumer holds allowed models, quotas, context cap, max output, concurrency, expiry and allowed request properties. A key can narrow it.
  - Keys are shown once and stored as a fast hash plus a visible prefix. They can expire, be revoked and be bound to a Tailscale node through WhoIs.
- **Passwords:** argon2id at or above OWASP's minimum (m=19 MiB, t=2, p=1, [OWASP](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)).
- **IDs:** all IDs are UUIDv7, so data from several hosts can merge later.
- **Policy:** rules (request properties, time windows, priority class, caps) use our own versioned declarative format behind a `Policy` interface, which separates the decision from enforcement. Hosts edit rules through forms.
- **Judgement:** high-entropy keys need only a fast hash, while passwords need a slow one.
- **Unsourced:** the binary-size cost of CEL or Rego.

## Admission and scheduling (D-26)

- **Priority classes:** two, interactive ahead of background. Background work ages up in priority so it is never starved.
- **Concurrency:** limited per grant, and the total stays at or below `OLLAMA_NUM_PARALLEL` ([FAQ](https://docs.ollama.com/faq)).
- **Memory admission:** the loaded size (`/api/ps`) plus the estimated context must fit the share budget.
- **Bounded queue:** it fails fast with 429/503 and `Retry-After`, so Ollama's own queue never fills. A running request is never interrupted. **Judgement:** Ollama documents no way to interrupt.
- **Background jobs:** they may use `POST /v1/jobs` (returns a job id; the client polls or gets a webhook) with an `Idempotency-Key`, so client retries never run twice. Job results are kept apart from logs, with a short TTL, and deleted on fetch.

## Usage ledger and receipts (D-27)

- **Ledger:** a Merkle-tree log in SQLite using RFC 6962 math ([RFC 6962](https://www.rfc-editor.org/rfc/rfc6962.html), [RFC 9162](https://www.rfc-editor.org/rfc/rfc9162.html)) via [`x/mod/sumdb/tlog`](https://pkg.go.dev/golang.org/x/mod/sumdb/tlog). Signed checkpoints use [C2SP tlog-checkpoint](https://c2sp.org/tlog-checkpoint). It gives inclusion and consistency proofs. Witness co-signing ([tlog-witness](https://c2sp.org/tlog-witness)) can come later.
- **Receipts:** JWS with the fully specified `Ed25519` alg ([RFC 9864](https://www.rfc-editor.org/rfc/rfc9864.html), [RFC 8037](https://www.rfc-editor.org/rfc/rfc8037.html), [RFC 7515](https://www.rfc-editor.org/rfc/rfc7515.html)). They go in the `LS-Receipt` header, or in the final SSE event when streaming, and carry their checkpoint.
- **Write path:** the usage record and ledger entry are appended synchronously at request end through one batched writer. Minute/hour/day rollups are computed asynchronously.
- **Keys:** the host identity key signs a rotating receipt-signing subkey. Public keys are published at `/v1/keys` ([crypto/ed25519](https://pkg.go.dev/crypto/ed25519)).
- **Why not a plain hash chain:** it shows tampering only to someone who audits it and keeps old heads ([Crosby & Wallach 2009](https://www.usenix.org/legacy/event/sec09/tech/full_papers/crosby.pdf)).
- **Rejected:** [Tessera](https://github.com/transparency-dev/tessera), which needs other storage, and [Trillian](https://github.com/google/trillian), which is in maintenance mode and needs MySQL. The log also cites COSE ([RFC 9052](https://www.rfc-editor.org/rfc/rfc9052.html)) as a source.
- **Judgement:** the rotating subkey limits a leaked key's damage without changing the host identity.

## Storage (D-28, D-44)

- **Driver:** SQLite through [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite). It is pure Go, so `CGO_ENABLED=0` cross-compiles for 3 OSes on amd64/arm64.
  - Why pure Go: it keeps the 3-OS curl-install release matrix cheap.
  - Other drivers the log cites: [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) (cgo, [cmd/cgo](https://pkg.go.dev/cmd/cgo)) and [ncruces/go-sqlite3](https://github.com/ncruces/go-sqlite3).
  - **Judgement:** modernc is about 1.3–2× slower on CPU-heavy queries ([go-sqlite-bench](https://github.com/cvilsmeier/go-sqlite-bench)), which doesn't matter at a few requests per second.
- **Concurrency:** WAL mode ([WAL](https://www.sqlite.org/wal.html)). One writer goroutine using `BEGIN IMMEDIATE` ([transactions](https://www.sqlite.org/lang_transaction.html)), a small read pool and `busy_timeout` ([busy_timeout](https://www.sqlite.org/c3ref/busy_timeout.html)).
- **Ownership:** each module owns its tables behind its own repository interface. Migrations are built in and run at startup. A Postgres or hosted adapter can implement the same repositories later.
- **Conventions (D-44, from the superseded D-37):**
  - `STRICT` tables ([STRICT](https://www.sqlite.org/stricttables.html)), with `foreign_keys=ON` ([FKs](https://www.sqlite.org/foreignkeys.html)) and `busy_timeout` set in every DSN.
  - FKs use `RESTRICT` from soft-deleted parents and `CASCADE` for owned children.
  - UUIDv7 stored as TEXT ([RFC 9562](https://www.rfc-editor.org/rfc/rfc9562.html), [Go 1.27](https://go.dev/doc/go1.27), [uuid](https://pkg.go.dev/uuid)). Times are INTEGER Unix ms UTC. Only portable types are used.
- **Module prefixes:** `hst_` host, `idn_` identity, `grt_` grants, `req_` requests, `sch_` scheduler, `use_` usage, `ldg_` ledger, `ntf_` notify, `aud_` audit, `syn_` sync, `plt_` platform, `ops_` ops.
- **Metadata on every table and column:** a plain-language comment, a classification (`public`/`internal`/`personal`/`sensitive`/`content`) and a `billable` flag. These are collected into `ops_data_catalog` at startup. A generated schema reference doc is kept in sync.
- **Migrations:** forward-only. Destructive changes go expand → backfill → contract across two releases. Exports and sync changes carry the schema version, and import refuses unknown versions.
- **Retention defaults (each a setting, D-38):**
  - rollups: minute 7 d / hour 90 d / day forever
  - delivered outbox: 7 d
  - notifications: 90 d
  - job result: on fetch or 24 h
  - sync change: 7 d after ack
  - host metrics: 7 d
  - logs: 14 d / 50 MB

### Relationships (D-42, from the superseded D-34)

- Anything usage, the ledger or receipts refer to is soft-deleted.
- **Host:** host 1:N `hst_key` (an identity root plus receipt subkeys; retired, never deleted).
- **Identity:**
  - consumer 1:0..N credential (0 = key-only).
  - invite → 0..1 redeemed consumer.
  - consumer 1:N access key.
- **Grants:**
  - consumer 1:1 grant.
  - grant 1:N immutable grant version.
  - grant version N:M model (per-model context and output caps).
  - policy rule → 0..1 grant (none means global).
- **Requests:** consumer 1:N resource request 1:N comment. A request → 0..1 resulting grant version.
- **Jobs and usage:**
  - access key 1:N job 1:0..1 job result.
  - access key 1:N usage record. The record is immutable and also copies the consumer and grant version.
  - usage record 1:1 ledger leaf.
  - A checkpoint covers leaves `[0, tree_size)`.
- **Notifications:** principal 1:N notification / preference / webhook endpoint. Event 1:N outbox delivery.
- **Audit:** audit events refer to principals and are append-only.
- **Sharing:**
  - `plt_dataset_key` 1:N `plt_sharing_grant` (RESTRICT).
  - A grant has one grantor and one grantee principal.
  - `idn_consent` N:1 principal.
- **Judgement:** copying `consumer_id` and `grant_version_id` into usage rows avoids hot-path joins.

## Off-host backups and data sharing (D-41, D-43)

- **Default (decentralised):**
  - An `OffHostSync` port takes whole-database snapshots: `VACUUM INTO` ([VACUUM](https://www.sqlite.org/lang_vacuum.html)) → compress → encrypt on the host.
  - Snapshots go to an S3-compatible bucket the host owns (R2 free tier, [R2 pricing](https://developers.cloudflare.com/r2/pricing/)).
  - Scheduled backups can also go to a host-chosen local folder (D-28).
  - Interval: host-configurable, 5 min proposed. It always runs off the request path.
- **Opt-in platform sync (per host):**
  - Encrypted change rows (`host_id, dataset, table, pk, version, ciphertext`) go to the platform admin's Supabase Postgres through the session pooler.
  - Supabase free tier: 500 MB, and projects pause after a week inactive; Pro is about $25/mo ([pricing](https://supabase.com/pricing), [pausing](https://supabase.com/docs/guides/platform/free-project-pausing)).
  - It is needed only for platform features such as sharing grants.
- **Tooling:**
  - a one-command `restore`
  - `export`/`import` of signed, versioned NDJSON bundles per table
  - `ops_schema_migrations` plus a migration run log
  - the host data key shown once as a recovery code
- **Later:** Litestream ([config](https://litestream.io/reference/config/)) is a possible later implementation.
- **Sharing grant:**
  - A grantor (e.g. a host) grants a grantee access to a named dataset. Each grant carries a purpose, the terms version and an expiry.
  - Each dataset has its own data key, wrapped to the grantee's X25519 public key.
  - Expiry or revocation rotates the dataset key. Data already seen cannot be withdrawn.
  - Every change is audited and notified.
  - **Phase 1 grantee:** only the platform admin, and only for hosts that opted into platform sync. Host ↔ consumer sharing is a future use.
- **Datasets:** usage, audit and consumer directory.
- **Never leaves the host:** job content and results, sessions, credential secrets and private keys.

## Notifications (D-29)

- **Routing:** one `Notifier` port. Domain events go through per-principal preferences to channels.
- **In-app:** stored in SQLite and pushed over SSE.
- **Webhooks:** in the Standard Webhooks format ([spec](https://raw.githubusercontent.com/standard-webhooks/standard-webhooks/main/spec/standard-webhooks.md), [site](https://www.standardwebhooks.com/)), signed with HMAC-SHA256 `v1` using one secret per endpoint.
  - Delivery is at least once, through a transactional outbox, with exponential backoff.
  - A `410` disables the endpoint. Receivers de-duplicate by `webhook-id`.
- **Desktop:** [beeep](https://pkg.go.dev/github.com/gen2brain/beeep), only when running in the user's session. Windows services in session 0 cannot show UI ([interactive services](https://learn.microsoft.com/en-us/windows/win32/services/interactive-services)).
- Email and Telegram come later as adapters.
- **Unsourced:** receiver library support for Ed25519 `v1a`.

## Packaging and OS integration (D-30)

- **Service:**
  - **Default:** a per-user login service with no admin rights: macOS LaunchAgent, `systemd --user` with linger and a custom unit, or a Windows logon task.
  - **Optional:** a system service at boot, plus a user-session helper for notifications.
  - Both modes go through [kardianos/service](https://pkg.go.dev/github.com/kardianos/service) ([darwin](https://raw.githubusercontent.com/kardianos/service/v1.3.0/service_darwin.go), [systemd](https://raw.githubusercontent.com/kardianos/service/v1.3.0/service_systemd_linux.go)) behind a service interface ([launchd](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html)).
  - The default matches where Ollama runs on macOS and Windows, as a user app ([Windows](https://docs.ollama.com/windows), [macOS](https://docs.ollama.com/macos), [Linux](https://docs.ollama.com/linux)).
- **Installers:** `curl … | sh` and PowerShell `irm … | iex`. They:
  - download the release binary and verify its SHA-256
  - install into a user folder on PATH
  - register the service
  - detect Ollama and Tailscale and offer to install each

  Uninstall and upgrade are in the same script.
- **Sleep lock:** a `Power` port: IOPM assertion / `caffeinate -i` on macOS ([QA1340](https://developer.apple.com/library/archive/qa/qa1340/_index.html), [caffeinate](https://ss64.com/mac/caffeinate.html)), [`SetThreadExecutionState`](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-setthreadexecutionstate) on Windows, and a logind inhibitor on Linux (best effort) ([inhibitor locks](https://systemd.io/INHIBITOR_LOCKS/), [native/power](https://pkg.go.dev/github.com/crgimenes/native/power)).
  - It is held only while sharing is on and not paused.
  - The UI warns that closing the lid and manual sleep can't be blocked.
- **Gatekeeper (F-07):**
  - curl does not set the quarantine flag, so a curl-downloaded binary isn't checked by Gatekeeper. A browser download would be blocked, so the installers fetch with curl only ([Apple forums](https://developer.apple.com/forums/thread/788787), [Eclectic Light](https://eclecticlight.co/2020/10/29/quarantine-and-the-quarantine-flag/)).
  - Go's linker ad-hoc signs darwin/arm64 binaries ([go#42684](https://github.com/golang/go/issues/42684)), so the release pipeline never strips or alters a built darwin binary.
  - **Unsourced:** a LaunchAgent running it on macOS 15/26, and amd64 needing no signature. Test before the first release.
- **Windows engine install (D-49, F-10):** after the host's yes, `OllamaSetup.exe /SILENT`. It shows a progress window, installs per user with no admin, and Ollama starts when it finishes ([ollama.iss](https://raw.githubusercontent.com/ollama/ollama/main/app/ollama.iss), [Inno Setup flags](https://jrsoftware.org/ishelp/topic_setupcmdline.htm)). **Unsourced:** the launch after a silent install was read from ollama.iss, not observed (D-49).

## Observability and maintainability (D-31)

- **Telemetry port:** modules call a thin `Telemetry` port.
  - Default: [`log/slog`](https://pkg.go.dev/log/slog) JSON logs plus a Prometheus `/metrics` endpoint on the admin listener ([client_golang](https://github.com/prometheus/client_golang)). Spans are a no-op.
  - An OpenTelemetry adapter can be switched in later ([releases](https://github.com/open-telemetry/opentelemetry-go/releases), [README](https://raw.githubusercontent.com/open-telemetry/opentelemetry-go/main/README.md), [otelslog](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog)).
  - A request id ties signals together.
  - Prompts and responses are never logged, and redaction uses typed `LogValuer`s.
- **HostMetrics port:** built on [gopsutil](https://github.com/shirou/gopsutil) ([process](https://pkg.go.dev/github.com/shirou/gopsutil/v4/process)).
  - It reports host totals, the share-budget slice (local-server, Ollama and loaded models) and other apps, each labelled with how it is measured.
  - Processes it can't read count as "unattributed" ([process_windows](https://raw.githubusercontent.com/shirou/gopsutil/master/process/process_windows.go), [process_darwin](https://raw.githubusercontent.com/shirou/gopsutil/master/process/process_darwin.go), [mem_darwin](https://raw.githubusercontent.com/shirou/gopsutil/master/mem/mem_darwin.go), [htop #1976](https://github.com/htop-dev/htop/pull/1976)).
- **Maintainability:**
  - module boundaries checked in CI
  - one contract test suite per port that every adapter must pass, plus a fake engine
  - golden tests for the OpenAI-compatible contract
  - CI on 3 OSes
  - an architecture note per module
- SLO targets (gateway overhead, admission latency, receipt signing) are set in LLD (D-31). No LLD row sets them yet; they are owed before PLT-F-018 builds.
- **Unsourced:** the OTel SDK's binary-size cost.

## API contract (D-32, D-33)

- **Spec-first:** OpenAPI 3.1. Server interfaces are generated from the spec, and PA's adapter tests check against it.
- **Standard surfaces, implemented as published and never modified:**
  - OpenAI-compatible `/v1/chat/completions` (streaming, `stream_options.include_usage`), `/v1/embeddings` and `/v1/models` ([Ollama OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility))
  - Standard Webhooks
  - JWS receipts
  - C2SP checkpoints
- **Additive local-server contracts:**
  - `/v1/capabilities`, `/v1/health`, `/v1/jobs`, `/v1/resource-requests` (with comments) and `/v1/usage`
  - `/v1/keys`, `/v1/ledger/checkpoint` and `/v1/usage/{id}/proof`
  - notification preferences and webhook endpoints
  - The admin API `/admin/v1` is on the admin listener only.
- **Errors:** the OpenAI error envelope plus typed codes: `quota_exceeded`, `host_paused`, `model_not_granted`, `context_too_long`, `queue_full`, `engine_unavailable`. They come with `Retry-After`. Unsupported OpenAI fields (`n`, `logit_bias`, `tool_choice`, …) are rejected, never silently dropped.
- **Versioning:** the major version is in the URL, and changes within a major are additive only. Deprecations use `Deprecation`/`Sunset` headers. Golden tests guard compatibility.
- **Extensions on OpenAI-compatible calls go in headers:** request properties come in on `LS-Request-Properties`, and the receipt goes out on `LS-Receipt`. When streaming, the receipt and usage also come in a final SSE event that standard clients skip.
- **Judgement:** explicit rejection, additive-only versioning, and headers over body fields.
- **Unsourced:** how SDKs and proxies handle unknown body fields.

## State machines (D-38)

Terminal states are never reopened, so audit and billing history stays linear.

| Entity | States and transitions |
|---|---|
| Job | queued → running → succeeded \| failed \| cancelled. A queued job can also expire. While paused or quiet it stays queued. On restart, running → failed(`host_restart`). **Judgement:** Ollama cannot resume a generation. |
| Interactive request (in memory) | received → rejected \| queued → running → completed \| aborted (client disconnect cancels the engine call; tokens so far are metered) |
| Resource request | open (comments) → approved (new grant version) \| rejected \| withdrawn \| expired |
| Access key | active → expired \| revoked |
| Invite | pending → redeemed \| expired \| revoked |
| Consumer | active ↔ suspended → deleted (soft; keys revoked) |
| Host sharing | sharing ↔ paused (manual) and sharing ↔ quiet (schedule). Interactive calls get `host_paused`, jobs wait and the sleep lock is released. |
| Model (from `/api/ps`) | unloaded → loading → loaded → unloading → unloaded. A failed load goes back to unloaded (`load_failed`). |
| Webhook delivery / endpoint | pending → delivering → delivered \| retry_scheduled \| failed. The endpoint is disabled after a `410` or N failures. |
| Sharing grant | requested → active \| declined. active → expired \| revoked (the dataset key rotates). |
| Signing key | active → retired (kept for verification) |
| Sync change | pending → acked → pruned |

**Configurability:**
- Every time, retention, TTL, retry and threshold value is a stored setting with a default, editable at runtime by the party that owns it. Nothing is hard-coded.
- Owners: the platform admin owns platform-target settings and minimums, the host owns host data, and the consumer owns its own preferences.
- A lower party can tighten a value only within the higher party's bounds.

## Call flows (D-39)

- **F1 interactive (sync):**
  1. The request arrives over the transport.
  2. Auth: `Bearer ls_<prefix>_<secret>` → lookup by prefix → constant-time hash compare. The key, consumer and grant come from a cache that change events clear. An optional WhoIs check confirms the bound node.
  3. `Policy.Evaluate` runs.
  4. Validate: unsupported fields are rejected, `max_tokens` is capped, and the prompt size is pre-checked (an estimate; **Judgement**).
  5. `Scheduler.Admit`.
  6. `Engine.Chat` calls native `/api/chat` with `num_ctx`=cap, `num_predict` and `keep_alive`, under first-token, idle and total timeouts.
  7. The answer streams back as OpenAI SSE. A client disconnect cancels the call.
  8. One `BEGIN IMMEDIATE` batch writes the usage record, ledger leaf/hashes and sync change. Then the JWS receipt is sent.
  9. If metering fails it is retried. If it keeps failing, the answer is still returned and the failure is logged and alerted (experience first, user-confirmed).

  An engine that's down returns 503 `engine_unavailable`.
- **Context overflow (D-48, F-09):**
  - Ollama silently drops the oldest messages by default ([prompt.go](https://raw.githubusercontent.com/ollama/ollama/main/server/prompt.go)).
  - Each grant version sets `on_context_overflow`: `refuse` (default) or `truncate`.
  - `refuse` sends `truncate:false` and `shift:false` on `/api/chat` ([types.go](https://raw.githubusercontent.com/ollama/ollama/main/api/types.go); undocumented) and `truncate:false` on `/api/embed` ([embed](https://docs.ollama.com/api/embed)). The error maps to `context_too_long`.
  - A minimum Ollama version is pinned, and a contract test checks the refuse behaviour.
  - Embedding can error even with truncation on ([#14186](https://github.com/ollama/ollama/issues/14186)), so the adapter handles that error.
- **F2 job:** `POST /v1/jobs` with an Idempotency-Key (a duplicate returns the existing job) → F1 steps 2–4 → queued → 202. A worker runs it in priority/aging order through F1 steps 5–8. The result is encrypted, then `job.done` → outbox. Results are fetched from `GET /v1/jobs/{id}` and `…/result` (deleted on fetch, per setting).
- **F3 resource request:** the consumer posts → the host is notified. On approve, one transaction writes the new grant version, the current pointer, the status, the audit event, the notification event and the sync change. Then the cache is cleared and the consumer is notified.
- **F4 webhooks:** due outbox rows → signed POST ([Standard Webhooks](https://raw.githubusercontent.com/standard-webhooks/standard-webhooks/main/spec/standard-webhooks.md)) → status.
- **F5 checkpoint:** every N leaves or T seconds → tlog root → C2SP signed note → `ldg_checkpoint`.
- **F6 off-host:** a snapshot (`VACUUM INTO` → compress → encrypt → upload → verify → prune, [VACUUM](https://www.sqlite.org/lang_vacuum.html)). If opted in, also change rows past the cursor → AEAD per dataset → upsert → advance the cursor.
- **F7 sharing grant:** the grantee's tool writes a request → the host pulls it → host UI → approve → the key is wrapped to the grantee → pushed → decrypted locally. A timer rotates the key at expiry or revocation.
- **F8 login:**
  - Password: argon2id → session cookie (HttpOnly, Secure, SameSite=Strict).
  - Passkey: via [go-webauthn](https://pkg.go.dev/github.com/go-webauthn/webauthn/webauthn).
  - Invite: token → consumer + first credential.
- **F9 startup:** migrations → keys → check Ollama/Tailscale → listeners → workers (scheduler, outbox, checkpoint, sync, retention, power lock).

## Data paths (D-40)

| Data | Path and retention |
|---|---|
| Prompt/response content | Client → encrypted transport → gateway memory → Ollama on localhost → back. Never on disk or in logs. |
| Queued job content | `sch_job_payload`, encrypted with the host data key. Deleted at the terminal state. Never synced. |
| Job result | `sch_job_result`, encrypted. Deleted on fetch or after 24 h. Only the job's consumer can fetch it. |
| Access key secret | Shown once. Only the prefix and hash are stored. Never synced. |
| Credentials | argon2id hash or WebAuthn public key. Never synced. |
| Consumer personal data | `idn_consumer`. Personal fields are erased on deletion, and the row stays for ledger references. Synced encrypted in the consumer-directory dataset. |
| Usage metadata | Immutable record → ledger leaf → rollups. "Usage" dataset. |
| Audit events | Forever. "Audit" dataset. |
| Host resource readings | Every N s → `hst_metric_minute` (7 d) → `/metrics` and the UI. Not synced. |
| Logs | Rotating slog JSON files with redacted types. Not synced. |
| Webhook payloads | Events carry no content. Kept until delivered + 7 d. |
| Secret keys | Sealed with a host master key held in the OS keychain (macOS Keychain, Windows DPAPI/Credential Manager, Linux Secret Service), else a user-only file. Never leave the host, except dataset keys wrapped for an approved grantee. **Judgement:** the keychain-plus-file fallback. Library: [`zalando/go-keyring`](https://raw.githubusercontent.com/zalando/go-keyring/master/README.md), with no cgo on all three OSes (F-08). On Linux it errors without a Secret Service daemon (headless), so the file fallback is needed there. |

## Future direction: seams only (D-45)

Nothing here is built in Phase 1 unless a row above says so.

1. **User categorisation:** `idn_principal` tags and `plan_id`.
2. **Consent and profiles:** `idn_consent`. Phase 1 records only sign-up terms/privacy acceptance and sharing-grant consent. A content-capture interface is defined but off and not built, and may only ever run behind a matching consent record.
3. **Benchmarking and smart host selection:** hooks in `/v1/capabilities` and the `directory` port, with client-side multi-host failover.
4. **Party-to-party trace sharing:** `plt_sharing_grant`.
5. **Decentralised by default.**
6. **Legal traces:** ADRs, [executive-input records](../execetive_inputs/) and [`docs/legal/`](legal/README.md). Only the current phase's legal needs are discussed.
