# Glossary

One word, one meaning. This file is the single place where a term is defined. Other docs link here instead of redefining terms.

**Rules**
- Search here before naming anything new. If a term exists, use it. If it doesn't, add a row in the right section.
- **Users see** is the wording the product shows people. `—` means the term is internal and never shown to users.
- **Source** is the requirement line or decision row (`D-NN` in [`req_001.decisions.md`](tracks/platform/phase_1/requirements/req_001.decisions.md)) the term comes from.
- A rename keeps an old → new row under [Retired names](#retired-names). The old word is not used in new docs, UI or code.

## People and roles

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Host | The person who owns the machine and shares its AI resources. | Host | req_001 §2 glossary |
| Consumer | Someone allowed to use a host's resources. A consumer is an account of kind *individual* (a person) or *application* (an app acting on its own behalf). A *key-only consumer* has access keys but no login. | Consumer | req_001 §2 glossary, A4, D-13 |
| Client app | Software that calls a host on a consumer's behalf, e.g. PA. | App | req_001 §2 glossary |
| PA | The personal-assistant application, the first client app. It lives in a separate repo (`personal-assistant`). | — | req_001 formal §1, D-03 |
| Platform admin | The operator of the optional platform services (the project owner). They can see a host's data only through a sharing grant the host approved. | Platform admin | D-43 |
| Principal | Any actor with an identity: host admin, consumer, platform admin, or future kinds. Shared properties live on the principal, and kind-specific ones on the type table. | — | D-42 |

## Machines and software

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Host machine / Node | The computer running local-server (e.g. the Mac mini). | Host machine | req_001 §2 glossary |
| local-server | This application: the gateway and admin/consumer web pages that run on the host machine. | local-server | req_001 |
| Engine | The model runtime underneath local-server (Ollama by default; llama.cpp and MLX later). | Engine | req_001 §2 glossary, D-14 |
| Transport | How consumers reach the host machine over an encrypted channel. Phase 1 default: Tailscale. | Connection | D-12 |
| Fallback | The provider a client app uses when the host is offline, paused or declines (for PA: its Gemini/Anthropic adapters). | Fallback | D-03, D-11 |
| System Tailscale | The default transport mode: the host's normal Tailscale app carries traffic. | — | D-23 |
| Embedded Tailscale | An optional build of local-server with Tailscale (tsnet) inside the binary, so the host installs only one thing. | — | D-23 |
| Public mode | Opt-in, off by default. Exposes the consumer API on a public `*.ts.net` address through Tailscale Funnel, with TLS ending on the host machine. | Public access | D-24 |
| Port / adapter | A port is an interface local-server's modules depend on (`Engine`, transport, storage, auth, notify, directory, `Policy`, `Notifier` (the domain-event bus), `Channel` (notification delivery, D-84), `Telemetry`, `HostMetrics`, `Power`, `OffHostSync`). An adapter is one implementation of it (e.g. the Ollama adapter). | — | D-21 |
| Module | One internal part of local-server that owns its own packages, tables (by prefix) and port. D-21 names six top-level modules (gateway, control plane, scheduler, ledger, notifier, host-metrics); the LLD module map and the `tools/archcheck` config refine them into 28 modules (storage, settings, telemetry, host, engine, identity, grants, audit, events, auth, policy, scheduler, usage, ledger, limits, gateway, requests, notifier, transport, offhost, admin, consumerauth, bundle, sharing, power, service, directory, api). The control plane is identity, grants, auth, policy, requests, admin and consumerauth; host-metrics is part of host. D-44's twelve table prefixes (`hst_`, `idn_`, `grt_`, `req_`, `sch_`, `use_`, `ldg_`, `ntf_`, `aud_`, `syn_`, `plt_`, `ops_`) name the table owners among them. Each module has an architecture note in `docs/modules/`. | — | D-21, D-28, D-44 |
| Listener | One of local-server's three network endpoints: consumer API, consumer web page, and the admin listener, which serves the admin API (`/admin/v1`), `/metrics` and the admin UI. | — | D-10, D-21 |
| Service mode | How local-server runs in the background: *user* (starts at login, the default) or *system* (starts at boot). | Start at login / Start at boot | D-30 |
| Unit of work | One database transaction a module opens. Other modules' rows (audit, domain events, sync changes, outbox) join it, so they commit or roll back together. | — | D-28, D-39 |
| Single writer | The one goroutine in `internal/storage` that owns SQLite writes and commits queued units of work in batches; each unit runs in its own savepoint, so a failing unit rolls back alone. | Write lock | D-28 |
| Data catalog | The `ops_data_catalog` table: one row per table and column, with its module and meaning, read from `--@table` / `--@column` comments in each migration. Startup fails if an entry is missing. | Schema registry | D-44 |
| Request id | The id `internal/telemetry` puts on every request and log record; an inbound `X-Request-Id` is kept if well-formed, else one is generated. | Trace id, correlation id | D-31 |
| Redacted | The `internal/telemetry` type that wraps prompt or response content so it renders as `[redacted]` in logs, `fmt` and JSON, even inside another struct; code reads the real text only through `Reveal`. | — | D-31, D-40 |
| Domain event | A record that something happened (request decided, host paused, job done). It is written in the same unit of work as the change, then handed to subscribers such as notifications. | — | D-29 |
| Doctor | The `local-server doctor` command, which checks the engine version, keychain, ports and Tailscale, and shows the time of the last successful platform sync when sync is on. | Doctor | D-14, D-23, D-48, D-67 |
| Sleep lock | local-server keeps the host machine awake while the sharing state is *sharing*: not paused and not in quiet hours. | Keep awake | D-30, D-06, D-54 |
| Unattributed | Host resource use local-server cannot assign to a process because the OS won't report it; shown separately, never guessed. | Unattributed | D-31 |
| Tailnet | The private Tailscale network the host machine and its consumers' nodes share. | — | D-10, D-12 |
| Share-budget slice | The host resources local-server's sharing is using now: the local-server process, the Ollama processes and the loaded models. Its memory is local-server's RSS plus the larger of Ollama's RSS and the loaded models' host RAM, so weights are never counted twice. It is measured, not reserved. Other apps is host use outside it. | — | D-31, D-91 |
| Other apps | Host resource use outside the share-budget slice: total host use minus local-server, Ollama and loaded models (free memory is not counted). | Other apps | D-31, D-92 |
| Preload | Loading a model at start and when the preload setting changes, and keeping it loaded until it leaves the setting; a preloaded model is never unloaded for idleness or to make room. | Preload | D-22 |
| Keep-alive | How long an idle model stays loaded before local-server unloads it (idle unload). A negative value keeps it until it is unloaded by hand or to make room. | Keep alive | D-22 |
| Reclaim | Unloading idle models that are not preloaded, least recently used first, to make room for a request that does not fit the memory budget. Different from idle unload after the keep-alive. | — | D-22, D-26 |
| Data directory | The folder that holds the host's database and, when the keychain is not used, the master key file. It is `--data-dir`, else `LS_DATA_DIR`, else `local-server` in the user configuration directory, and it is the only thing configured outside the database. | Data directory | D-39, D-54 |
| First run | A start of `local-server serve` on a database with no host row. It creates the host, its keys and the master key, and prints the recovery code once to a terminal. With no terminal it refuses. | first run | D-101, D-102 |
| Worker supervisor | Runs each background worker of `serve`. It restarts a worker that panics, fails or returns early after `workers.restart_backoff`, and counts restarts in `supervisor_worker_restarts_total`. | — | D-39, D-54 |
| Fake engine | The engine adapter that answers without Ollama, for tests, local runs and demos (`serve --engine fake`). | — | D-31 |

## Access and limits

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Access key | A credential the host issues to a consumer to call the API. Shown once, can expire or be revoked. "API key" is an accepted synonym in conversation; docs and UI say *access key*. | Access key | req_001 §2 glossary |
| Invite | A one-time sign-up link the host sends so a person can create a consumer account. | Invite link | D-13 |
| Share budget | Everything the host sets aside for all consumers together (models, memory, concurrency, tokens). | Share budget | req_001 §2 glossary |
| Memory budget | The memory part of the share budget: bytes the host sets aside for loaded models and the context of running requests together (`scheduler.memory_budget_bytes`; 0 means no limit). A request that can never fit it is refused with `over_budget`. | Memory budget | D-26, D-93 |
| Grant | One consumer's part of the share budget: allowed models, quotas, context limit, output limit, concurrency, expiry and allowed request properties. | Your limits | req_001 §2 glossary |
| Grant version | An immutable snapshot of a grant's limits. Each change creates a new version, and usage records name the version they ran under. | — | D-25, D-42 |
| Host model | A model the engine has, with the host's sharing settings for it: whether new grants may include it (*allowed*), whether it is kept loaded (*preload*) and its keep-alive. | Models | req_001 D-25, D-22 |
| Context limit | The largest context window, in tokens, a grant allows for one model; local-server sends it to Ollama as num_ctx. The decisions log and tickets call it *context cap* (`context_cap` in the schema). | Context limit | D-22, D-25 |
| Request properties | Tags a consumer or client app attaches to a call (e.g. *background*), which the host's rules use to decide what the host machine does with it. | Request type | Q22, D-15 |
| Context overflow | What happens when a prompt is longer than the grant's context limit. Each grant sets it to *refuse* (the default, an error) or *truncate* (the oldest messages are dropped). | If a prompt is too long | D-48 |
| Background-work hours | A time window in which background requests run (e.g. 00:00–07:00), set per grant or for the whole host. Different from quiet hours. | Background hours | D-15 |
| Pause sharing | A switch that makes the host stop accepting new work from all consumers immediately; requests already running finish and queued jobs wait. Consumers see the host as *paused*. | Paused | D-06, D-26, D-38 |
| Quiet hours | A daily schedule during which the host stops serving automatically. The sharing state is *quiet*, separate from the manual *paused* state (a pause wins over quiet hours); consumers get the same paused error either way. | Quiet hours | D-06, D-54 |
| Host identity key | The Ed25519 key pair each install generates. Its public key is the host's global ID. | Host ID | D-25 |
| Receipt-signing key | A rotating key, signed by the host identity key, that signs usage receipts. | — | D-27 |
| Checkpoint-signing key | A rarely rotated key, signed by the host identity key, that signs ledger checkpoints. Separate from the receipt-signing keys. | — | D-65 |
| Key set | The host's public keys, current and retired, published at `/v1/keys` as a JWK Set: the host identity key, the receipt-signing keys and the checkpoint-signing keys, each signing key with its key endorsement. Retired keys stay listed so old receipts and checkpoints still verify. | — | D-27, D-65, D-94 |
| Key endorsement | The host identity key's signature over a signing key's certificate (host id, role, public key, creation time), published with the key in the key set (`ls_endorsement`) so anyone can check that the key belongs to the host. | — | D-96 |
| Verifier key | The signed-note text form `<name>+<hash>+<key>` of a checkpoint-signing key, which signed-note tools use to check a checkpoint; published in the key set as `ls_note_verifier_key`. | — | D-65, D-97 |
| Host master key | The key that seals every secret local-server keeps at rest (private keys, the host data key, credentials). When the host is first set up it goes into the OS keychain or, if the keychain cannot be used or does not answer within `hostkeys.keychain_timeout`, into a user-only file in the data directory, where it then stays. It is never in the database. | — | D-40, D-81, D-102 |
| Host data key | The key that encrypts queued job content, job results and backup snapshots. It is sealed with the host master key and shown once, at setup, as the recovery code (see Recovery code). | — | D-40, D-41 |
| Policy rule | A versioned, declarative rule the host edits through a form (request properties, time windows, priority class, caps). Set per grant or globally. | Rule | D-25 |
| Policy rule version | One immutable stored version of a policy rule: its name, order, format version and document. Changing the document, name or order stores a new version; switching the rule on or off does not. A deleted rule keeps its versions. | — | req_001 D-87 |
| Priority class | *Interactive* (someone is waiting) or *background* (can wait). Interactive runs first, and background ages up so it is never starved. | Interactive / Background | D-26 |
| Quota | A list of windowed entries on a grant or an access key. Each entry caps one metric (`tokens` or `requests`) in one quota window, and a list has at most one entry per metric and window. Going over an entry returns `quota_exceeded` with `Retry-After`, and a near-limit notice is sent first. The older single `quota` object is a deprecated alias for the entries whose window is `limits.quota_window`. | Quota | D-25, D-29, D-107 |
| Quota window | The fixed period one quota entry counts over: a whole number of seconds that a client chooses, within the host settings `limits.quota_window_min` and `limits.quota_window_max`. Windows are aligned to multiples of their length counted from 0001-01-01 00:00 UTC (so a length that divides 24 hours starts a window at every midnight UTC), and a call is charged to the window that holds the moment it was admitted. The host setting `limits.quota_window` (one day by default) is the window of the deprecated single `quota` object. Only a consumer with a chargeable billing mode may choose its own windows on its keys. | — | D-107, D-54 |
| Request counter | The count, kept in the `use_request_counter` table per consumer or access key and per quota window, of the calls the gateway admitted, metered or not; a requests entry reads it. It survives a restart, and a row is deleted once its window ended more than `limits.counter_retention` ago. Counts only, no content. | — | D-107, D-31 |
| Billing mode | A consumer's `none`, `prepaid` or `postpaid`, set by the host admin. *Chargeable* means prepaid or postpaid: such a consumer may set its own quota windows on its keys, and the host sets them for a consumer with `none`. It holds no balance, invoice or price; money stays with the later billing seam. | — | D-107, D-02 |
| Rate limit | The maximum number of requests a key may send per time window. Mandatory in public mode. | Rate limit | D-24 |
| Retention rule | A setting saying how long one kind of record is kept before the retention worker deletes it. | How long we keep … | D-38, D-40 |
| Setting owner | The one party (platform admin, host or consumer) responsible for a configurable value. It sets the value freely; there is no bounds hierarchy between parties. | — | D-38, D-54 |
| Capabilities | The caller's effective limits after key narrowing, plus remaining quota (`/v1/capabilities`). | — | D-32 |
| Access key narrowing | Limits a key adds on top of its consumer's grant. They can only narrow the grant, never widen it. | — | D-25 |
| Bound node | The tailnet node an access key is restricted to, checked through Tailscale WhoIs. | — | D-39 |
| Lockout | An access key's source address or key prefix, or a login, blocked after repeated failures, for durations set by the host's lockout settings. A successful key check clears only its key prefix's counter. | — | D-24, D-99 |
| Session | What a login leaves in place: the browser holds a session cookie (`ls_admin_session` on the admin API, `ls_session` for consumers) whose random token the host stores only as a hash. It ends at logout or when the session TTL setting (`admin.session_ttl` for admins) runs out. | login | D-39, D-25 |
| Passkey ceremony | A started WebAuthn registration or login, with a ceremony id, that expires after the host's ceremony TTL setting. | — | D-13 |

## Requests and records

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Resource request | A consumer asking the host for more (another model, a higher quota, …). The host approves or rejects it, and both sides can comment. | Request | req_001 §2 glossary |
| Host status | The health signal a client app checks with its access key: online; sharing, paused or quiet; whether the engine answers; the loaded models it may use; queue depth. | Host status | D-05 |
| Usage ledger | The host's append-only, tamper-evident record of every metered request. | Usage history | D-16 |
| Usage receipt | A record of one request's usage, signed by the host and returned with the response, so a consumer can check a bill independently. | Receipt | D-16 |
| SLO target | A host-owned 95th-percentile time the gateway is held to: its own time per call, admission time and receipt-signing time (the gateway.slo_* settings). The SLO tests check them, and the gateway counts calls over the overhead target and slow unmetered-receipt signings in gateway_slo_breaches_total. | — | D-31, D-55 |
| Stream timeout | One of three host-owned limits on a streamed chat call's engine call (the gateway.stream_* settings): *first-token*, from the request to the engine until its first chunk; *idle*, the longest wait for the next chunk; *total*, from the request until the last chunk. Each cancels the engine call. A first-token timeout answers with a 504; a later one ends the stream with an error event, the failed call's receipt and [DONE]. | — | D-39, D-54 |
| Warnings | A list of short texts in an admin API answer to an issue or change, each naming a part of the request the host accepted but did not apply as sent, for example the deprecated `quota` sent beside `quotas`. Today it is on access keys and grant versions. Naming ignored unknown fields this way on the other admin request bodies is planned. | — | D-105, D-107 |
| Audit log | The record of admin actions: keys issued or revoked, grants changed, requests decided. | Activity log | D-07 |
| Job | A background request submitted to `/v1/jobs`. It runs when the host admits it, and the result is fetched later. | Background job | D-26, D-38 |
| Idempotency key | A client-chosen id on a job submission, so a retried submission never runs twice. | — | D-26 |
| Job result lifetime | How long a finished job's result stays fetchable if no fetch delivers it whole: the consumer-owned setting `jobs.result_ttl` (24 hours by default). A job's own time limit is `jobs.deadline`. | Result TTL | D-85, D-38 |
| Checkpoint | A signed snapshot of the usage ledger's size and root hash, made in the background. A receipt carries its leaf index and tree size; the consumer later fetches the first checkpoint covering that leaf with an inclusion proof, and keeps checkpoints so consistency proofs can show the ledger only grew. | — | D-27, D-52 |
| Unmetered receipt | A usage receipt marked *unmetered*: the host answered but could not record the call, so it carries the engine's token counts and no leaf index; signed evidence the call can't be billed later. | Unmetered | D-51 |
| Inclusion proof | A short proof that one usage record's leaf is in the usage ledger, checked against a signed checkpoint. | — | D-27, D-52 |
| Consistency proof | A proof that a later checkpoint's ledger contains an earlier one's unchanged, so the ledger only grew between them. | — | D-27 |
| Checkpoint origin | The first line of every checkpoint, also the checkpoint-signing key's name in the signed note. Defaults to `local-server/ledger/<host id>` and cannot change once the first checkpoint is signed. | — | D-97 |
| Rollup | Usage totals per minute, hour or day, computed from usage records. | Usage over time | D-27 |
| Granularity | The size of a rollup bucket: minute, hour or day. The `granularity` parameter of the usage endpoints. | Granularity | D-27, D-40 |
| Rollup bucket | The UTC minute, hour or day that one rollup row sums, identified by its `bucket_start`. Not the storage bucket an off-host backup is sent to. | — | D-27 |
| Outbox | The table where an event and its pending webhook deliveries are written in the same transaction, so none are lost. | — | D-29 |
| Webhook endpoint | A URL a consumer or host registers to receive signed event notifications. | Webhook | D-29 |

## Data and sharing

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Dataset | A named group of tables that can be shared or synced as one unit (usage, audit, consumer directory). Each has its own encryption key. | Data set | D-43 |
| Sharing grant | A grantor's time-boxed permission for a grantee to read one dataset, with a stated purpose and terms version. It can be revoked. | Data sharing | D-43 |
| Consent record | A stored acceptance of a policy (terms, privacy, a sharing grant) with its version and evidence. It can be withdrawn. | Consent | D-44, D-45 |
| Content capture | The port (`ContentCapture` in `internal/identity`) through which prompts or responses could be stored, only behind a matching, unwithdrawn consent record. Defined and off in Phase 1: it has no adapter. | Content logging | D-31, D-40, D-45 |
| Data classification | The label on every table and column: `public`, `internal`, `personal`, `sensitive` or `content`, plus a *billable* flag. | — | D-44 |
| Off-host backup | Encrypted database snapshots the host machine sends to a folder or a bucket the host owns. The default. Which tables are minimized (left empty) is the host's choice (D-64). | Backup | D-41, D-64 |
| Backup minimization | The host's per-table backup choice: *minimize* (rows left out of the snapshot) or *keep as is*. Job content and sessions are minimized by default. A table can be minimized only when every table that references it is minimized too (D-69). | Leave out of backups | D-64, D-69 |
| Platform sync | Opt-in per host. Encrypted change rows sent to the platform's database, needed only for platform features such as sharing grants. | Platform sync | D-41 |
| Recovery code | The host data key, shown once at setup, needed to restore encrypted backups. The host can see it again with `local-server recovery-code`, which needs a terminal and the admin login and writes an audit row. | Recovery code | D-41, D-80, D-101 |
| Module table prefix | The short prefix on every table name naming its owning module: `hst_` host (including host-local configuration such as `hst_setting`, D-70), `idn_` identity, `grt_` grants, `req_` requests, `sch_` scheduler, `use_` usage, `ldg_` ledger, `ntf_` notify, `aud_` audit, `syn_` sync, `plt_` platform, `ops_` ops. | — | D-44, D-70 |

## Retired names

| Old | New | Why |
|---|---|---|
| host user | Host | Ambiguous with consumer (req_001). |
| user user | Consumer | Ambiguous with host (req_001). |
| reserved resources | Share budget | req_001 §2 glossary. |
| platform admin approval | Sharing grant | Generalised to any grantor and grantee (D-43 supersedes D-36). |
| `Notifier` (notification-delivery port) | `Channel` | `Notifier` names the domain-event bus only (D-84, PLT-B-016, 2026-10-31). |
| `platform_`, `identity_`, … table prefixes | `plt_`, `idn_`, … | Short prefixes (D-44 supersedes D-37). |
