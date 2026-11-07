# Schema reference

Generated from the migrations by `go generate ./cmd/local-server` (storage module, D-44). Do not edit by hand.
Classification: public, internal, personal, sensitive or content. Billable: whether the data may be billed for later.

## aud_event

Module: audit. Classification: internal. Billable: false.

One thing that happened on this host, such as a key issued or a grant changed, kept forever.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Event id, UUIDv7. |
| action | TEXT | no |  | internal | false | What happened, a dotted name such as grant.changed. |
| actor_id | TEXT | yes |  | internal | false | Principal who did it; empty when the system did it. |
| target_id | TEXT | yes |  | internal | false | Principal it was done to; empty when it has no principal target. |
| detail | TEXT | no |  | internal | false | Structured JSON object with the facts of the event; never holds prompts, responses or secrets. |
| created_at | INTEGER | no |  | internal | false | When it happened, Unix milliseconds UTC. |

## grt_grant

Module: grants. Classification: internal. Billable: true.

The grant of one consumer: what it may use. The terms live in immutable versions.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Grant id, UUIDv7. |
| consumer_id | TEXT | no |  | internal | false | Consumer the grant belongs to, one grant per consumer. |
| current_version_id | TEXT | yes |  | internal | false | The live version; empty until the first version is made. |
| created_at | INTEGER | no |  | internal | false | When the grant was created, Unix milliseconds UTC. |
| updated_at | INTEGER | no |  | internal | false | When the live version last changed, Unix milliseconds UTC. |

## grt_model

Module: grants. Classification: internal. Billable: false.

A model name a grant can allow, as the engine names it.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Model id, UUIDv7. |
| name | TEXT | no |  | internal | false | Model name in the engine, unique. |
| created_at | INTEGER | no |  | internal | false | When the model was added, Unix milliseconds UTC. |
| retired_at | INTEGER | yes |  | internal | false | When it was retired (soft delete), Unix milliseconds UTC; empty while in use. |
| allowed | INTEGER | no |  | internal | false | 1 when grants may include the model, 0 when the host has not opened it for sharing. |
| keep_alive_seconds | INTEGER | yes |  | internal | false | How long the model stays loaded when idle, in seconds; empty means the host's default keep-alive setting. |

## grt_policy_rule

Module: grants. Classification: internal. Billable: false.

A policy rule in the versioned declarative rule format; scoped to one grant or global.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Rule id, UUIDv7. |
| grant_id | TEXT | yes |  | internal | false | Grant the rule is scoped to; empty means it applies to every grant. |
| name | TEXT | no |  | internal | false | Short name the host gave the rule. |
| position | INTEGER | no |  | internal | false | Evaluation order, lowest first. |
| format_version | INTEGER | no |  | internal | false | Version of the rule format that body is written in. |
| body | TEXT | no |  | internal | false | The rule as JSON: request properties, time windows, priority class, caps. |
| enabled | INTEGER | no |  | internal | false | 1 when the rule is active, 0 when switched off. |
| created_at | INTEGER | no |  | internal | false | When the rule was created, Unix milliseconds UTC. |
| updated_at | INTEGER | no |  | internal | false | When the rule last changed, Unix milliseconds UTC. |
| version_no | INTEGER | no |  | internal | false | The live version of the rule in grt_policy_rule_version; name, position, format_version and body above are a copy of it. |
| deleted_at | INTEGER | yes |  | internal | false | When the rule was deleted (soft delete), Unix milliseconds UTC; empty while it applies. |

## grt_policy_rule_version

Module: grants. Classification: internal. Billable: false.

One immutable version of a policy rule: its name, order, format version and document. A change makes a new version.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| rule_id | TEXT | no | PK | internal | false | Rule the version belongs to. |
| version_no | INTEGER | no | PK | internal | false | 1, 2, 3 ... within the rule. |
| name | TEXT | no |  | internal | false | Short name the host gave the rule in this version. |
| position | INTEGER | no |  | internal | false | Evaluation order in this version, lowest first. |
| format_version | INTEGER | no |  | internal | false | Version of the rule format that body is written in, as the policy validator read it. |
| body | TEXT | no |  | internal | false | The rule document of this version, as JSON. |
| created_by | TEXT | yes |  | internal | false | Principal that stored the version; empty if the system did or the version came from before versions existed. |
| created_at | INTEGER | no |  | internal | false | When the version was stored, Unix milliseconds UTC. |

## grt_version

Module: grants. Classification: internal. Billable: true.

One immutable version of a grant: quotas, concurrency, expiry, allowed request properties and the context-overflow behaviour. A change makes a new version.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Version id, UUIDv7. |
| grant_id | TEXT | no |  | internal | false | Grant the version belongs to. |
| version_no | INTEGER | no |  | internal | false | 1, 2, 3 ... within the grant. |
| quota_tokens_per_day | INTEGER | yes |  | internal | true | Deprecated, read-only alias (req_001 D-107): a token limit for the window limits.quota_window, kept for versions made before grt_version_quota. New versions leave it empty and use grt_version_quota. |
| quota_requests_per_day | INTEGER | yes |  | internal | true | Deprecated, read-only alias (req_001 D-107): a request limit for the window limits.quota_window, kept for versions made before grt_version_quota. New versions leave it empty and use grt_version_quota. |
| max_concurrency | INTEGER | yes |  | internal | false | Most requests at the same time; empty means the host default. |
| expires_at | INTEGER | yes |  | internal | false | When the grant stops working, Unix milliseconds UTC; empty means never. |
| allowed_properties | TEXT | yes |  | internal | false | JSON list of the request properties or tags the grant allows; empty means any. |
| on_context_overflow | TEXT | no |  | internal | false | refuse (default) or truncate: what happens when a prompt is longer than the context cap (D-48). |
| reason | TEXT | yes |  | internal | false | Why the version was made, for example the approved resource request. |
| created_by | TEXT | yes |  | internal | false | Principal that made the version; empty if the system did. |
| created_at | INTEGER | no |  | internal | false | When the version was made, Unix milliseconds UTC. |

## grt_version_model

Module: grants. Classification: internal. Billable: false.

A model a grant version allows, with its per-model caps.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| version_id | TEXT | no | PK | internal | false | Grant version. |
| model_id | TEXT | no | PK | internal | false | Allowed model. |
| context_cap | INTEGER | yes |  | internal | false | Most context tokens for this model; empty means the host default. |
| max_output_tokens | INTEGER | yes |  | internal | false | Most output tokens for this model; empty means the host default. |

## grt_version_quota

Module: grants. Classification: internal. Billable: true.

One quota entry of a grant version: the most tokens or requests in one fixed window. Immutable, with its version.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| version_id | TEXT | no | PK | internal | false | Grant version the entry belongs to. |
| metric | TEXT | no | PK | internal | true | What is counted: tokens or requests. |
| quota_limit | INTEGER | no |  | internal | true | Most of the metric in one window; 0 allows none. |
| window_ms | INTEGER | no | PK | internal | true | Length of the quota window, a whole number of seconds in milliseconds (at least 1000). |

## hst_host

Module: host. Classification: internal. Billable: false.

This install's host: one row, whose id is the host's global id.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | public | false | The host's global id: the base64url text of the host identity public key (D-25). |
| created_at | INTEGER | no |  | internal | false | When the host identity was created, Unix milliseconds UTC. |
| data_key_sealed | BLOB | no |  | sensitive | false | The host data key sealed with the host master key; it protects queued job content and off-host copies. |
| sharing_state | TEXT | no |  | internal | false | The host's manual switch: sharing (serving) or paused (the host stopped serving by hand). Quiet hours are not stored here. |
| sharing_state_changed_at | INTEGER | yes |  | internal | false | When the switch last changed, Unix milliseconds UTC; empty until the first change. |

## hst_key

Module: host. Classification: internal. Billable: false.

An Ed25519 key of the host: the identity root, the receipt-signing subkeys and the checkpoint-signing keys, current and retired.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | public | false | Key id: the first 16 hex digits of the SHA-256 of the public key. |
| host_id | TEXT | no |  | internal | false | The host the key belongs to. |
| purpose | TEXT | no |  | internal | false | identity (the root), receipt (signs usage receipts) or checkpoint (signs ledger checkpoints). |
| public_key | BLOB | no |  | public | false | The 32-byte Ed25519 public key, published at /v1/keys. |
| private_sealed | BLOB | no |  | sensitive | false | The private key sealed with the host master key. |
| signature | BLOB | yes |  | public | false | The identity key's signature over this key's certificate; empty for the identity key itself. |
| created_at | INTEGER | no |  | internal | false | When the key was created, Unix milliseconds UTC. |
| retired_at | INTEGER | yes |  | internal | false | When the key stopped signing, Unix milliseconds UTC; empty while it is the live key. A retired key is kept so old signatures still verify. |

## hst_metric_minute

Module: host. Classification: internal. Billable: false.

The host's resource use, one row per minute: host totals, the share-budget slice (local-server, Ollama and loaded models), other apps and the unattributed process count. Not synced.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| minute | INTEGER | no | PK | internal | false | The start of the minute, Unix milliseconds UTC. |
| sampled_at | INTEGER | no |  | internal | false | When the last sample of the minute was taken, Unix milliseconds UTC. |
| mem_total_bytes | INTEGER | no |  | internal | false | Total memory of the host. |
| mem_used_bytes | INTEGER | no |  | internal | false | Memory in use on the host, as the OS counts it (this can differ from Activity Monitor on macOS). |
| cpu_cores | INTEGER | no |  | internal | false | Logical CPU cores of the host. |
| cpu_percent | REAL | no |  | internal | false | CPU use of the whole host, 0 to 100. |
| slice_mem_bytes | INTEGER | no |  | internal | false | Memory of the share-budget slice: local-server, Ollama and loaded models, never more than the host total. |
| slice_cpu_percent | REAL | no |  | internal | false | CPU of the slice as a share of the whole host, 0 to 100. |
| other_mem_bytes | INTEGER | no |  | internal | false | Memory used by other apps: the host total in use minus the slice, never below zero. |
| other_cpu_percent | REAL | no |  | internal | false | CPU used by other apps: host CPU minus the slice, never below zero. |
| unattributed | INTEGER | no |  | internal | false | How many processes the OS would not let local-server read; their use is counted in other apps. |
| slice_clamped | INTEGER | no |  | internal | false | 1 when the slice figures were cut down to the host total. |
| measures | TEXT | no |  | internal | false | JSON object saying how each value was measured (host_total, process_rss, engine_loaded, derived or unavailable). |

## hst_setting

Module: settings. Classification: internal. Billable: false.

One runtime setting value that its owner changed from the default; the rest use the default registered in code.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| key | TEXT | no | PK | internal | false | Dotted setting name, for example retention.interval. |
| scope | TEXT | no | PK | internal | false | What the value applies to, for example a consumer id for a consumer-owned setting; empty for a setting with one value. |
| value | TEXT | no |  | internal | false | The value as text; its type is the one registered for the key. |
| updated_by | TEXT | no |  | internal | false | Party that set it: platform_admin, host or consumer. |
| updated_at | INTEGER | no |  | internal | false | When it was set, Unix milliseconds UTC. |

## idn_access_key

Module: identity. Classification: sensitive. Billable: true.

An access key of a consumer. Only a visible prefix and a hash are stored; the secret is shown once and never synced.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Access key id, UUIDv7. |
| consumer_id | TEXT | no |  | internal | false | Consumer that holds the key. |
| prefix | TEXT | no |  | internal | false | Visible start of the key, unique; used to find the key. |
| secret_hash | TEXT | no |  | sensitive | false | Fast hash of the key secret. |
| label | TEXT | yes |  | personal | false | Name the consumer gave the key. |
| narrowing_json | TEXT | yes |  | internal | false | Optional JSON that narrows the consumer's grant for this key; empty means no narrowing. |
| bound_node | TEXT | yes |  | internal | false | Transport node the key is bound to; empty means not bound. |
| created_at | INTEGER | no |  | internal | false | When the key was created, Unix milliseconds UTC. |
| expires_at | INTEGER | yes |  | internal | false | When the key expires, Unix milliseconds UTC; empty means never. |
| revoked_at | INTEGER | yes |  | internal | false | When the key was revoked, Unix milliseconds UTC; empty while active. |

## idn_admin_user

Module: identity. Classification: personal. Billable: false.

A person who administers this host; the admin-specific properties of a principal.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Admin user id, UUIDv7. |
| principal_id | TEXT | no |  | internal | false | The principal this admin user is, one to one. |
| username | TEXT | no |  | personal | false | Login name, unique on this host. |
| display_name | TEXT | yes |  | personal | false | Name shown in the admin UI. |
| created_at | INTEGER | no |  | internal | false | When the row was created, Unix milliseconds UTC. |
| updated_at | INTEGER | no |  | internal | false | When the row last changed, Unix milliseconds UTC. |

## idn_consent

Module: identity. Classification: personal. Billable: false.

A consent a principal gave for one purpose under one version of the terms. Content capture may only run behind a matching, unwithdrawn record.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Consent id, UUIDv7. |
| principal_id | TEXT | no |  | internal | false | Principal that gave the consent. |
| purpose | TEXT | no |  | internal | false | What the consent covers, for example accepting the terms. |
| policy_version | TEXT | no |  | internal | false | Version of the terms or policy that was accepted. |
| granted_at | INTEGER | no |  | internal | false | When it was given, Unix milliseconds UTC. |
| withdrawn_at | INTEGER | yes |  | internal | false | When it was withdrawn, Unix milliseconds UTC; empty while in force. |
| evidence | TEXT | yes |  | personal | false | How the consent was given, for example the screen or request it came from; never prompts or responses. |

## idn_consumer

Module: identity. Classification: personal. Billable: true.

An individual or an application that uses this host; the consumer-specific properties of a principal. Personal fields are erased when it is deleted, the row stays for ledger references.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Consumer id, UUIDv7. |
| principal_id | TEXT | no |  | internal | false | The principal this consumer is, one to one. |
| kind | TEXT | no |  | internal | true | individual or application. |
| display_name | TEXT | yes |  | personal | false | Name of the person or application; erased on deletion. |
| email | TEXT | yes |  | personal | false | Contact email; erased on deletion. |
| row_version | INTEGER | no |  | internal | false | Increases with every change; recorded with the captured change. |
| created_at | INTEGER | no |  | internal | false | When the consumer was created, Unix milliseconds UTC. |
| updated_at | INTEGER | no |  | internal | false | When the consumer last changed, Unix milliseconds UTC. |
| billing_mode | TEXT | no |  | internal | true | none (default), prepaid or postpaid. Only a prepaid or postpaid consumer may set quota windows on its own keys; the host sets them for the rest. A label only: no balances, invoices or prices. |

## idn_credential

Module: identity. Classification: sensitive. Billable: false.

A way for a principal to log in: a password hash or a passkey public key. Never synced.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Credential id, UUIDv7. |
| principal_id | TEXT | no |  | internal | false | Principal the credential logs in; a key-only consumer has none. |
| kind | TEXT | no |  | internal | false | password or passkey. |
| identifier | TEXT | no |  | personal | false | Login name for a password, credential id for a passkey. |
| secret | TEXT | no |  | sensitive | false | Password hash (argon2id) or passkey public key; never the password itself. |
| created_at | INTEGER | no |  | internal | false | When the credential was added, Unix milliseconds UTC. |
| last_used_at | INTEGER | yes |  | internal | false | When it last logged in, Unix milliseconds UTC; empty if never. |
| revoked_at | INTEGER | yes |  | internal | false | When it was revoked, Unix milliseconds UTC; empty while usable. |

## idn_invite

Module: identity. Classification: sensitive. Billable: false.

An invitation for a new consumer. Pending until redeemed, revoked or expired.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Invite id, UUIDv7. |
| prefix | TEXT | no |  | internal | false | Visible start of the invite token, unique; used to find the invite. |
| token_hash | TEXT | no |  | sensitive | false | Hash of the invite token; the token is shown once and not stored. |
| created_by | TEXT | no |  | internal | false | Principal that issued the invite. |
| note | TEXT | yes |  | personal | false | Free-text note by the host, for example who it is for. |
| created_at | INTEGER | no |  | internal | false | When the invite was issued, Unix milliseconds UTC. |
| expires_at | INTEGER | no |  | internal | false | When the invite stops working, Unix milliseconds UTC. |
| redeemed_at | INTEGER | yes |  | internal | false | When it was redeemed, Unix milliseconds UTC; empty while pending. |
| redeemed_consumer_id | TEXT | yes |  | internal | false | Consumer created by redeeming it. |
| revoked_at | INTEGER | yes |  | internal | false | When the host revoked it, Unix milliseconds UTC; empty otherwise. |

## idn_principal

Module: identity. Classification: internal. Billable: true.

Every actor of the system (host admin, consumer, platform admin, future kinds) with the properties they share.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Principal id, UUIDv7. |
| kind | TEXT | no |  | internal | false | Actor kind: host_admin, consumer, platform_admin or a future kind; no fixed list so a new kind needs no migration. |
| category | TEXT | yes |  | internal | true | Category tag used to group principals, for example for a subscription strategy; empty when unused. |
| segment | TEXT | yes |  | internal | true | Segment tag used to group principals; empty when unused. |
| plan_id | TEXT | yes |  | internal | true | Placeholder for a future plan; always empty in Phase 1. |
| status | TEXT | no |  | internal | false | active, suspended or deleted; deleted is the soft delete. |
| created_at | INTEGER | no |  | internal | false | When the principal was created, Unix milliseconds UTC. |
| updated_at | INTEGER | no |  | internal | false | When the principal last changed, Unix milliseconds UTC. |

## idn_session

Module: identity. Classification: sensitive. Billable: false.

A logged-in web session of a principal. Only a hash of the session token is stored.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Session id, UUIDv7. |
| principal_id | TEXT | no |  | internal | false | Principal that is logged in. |
| token_hash | TEXT | no |  | sensitive | false | Hash of the session token. |
| created_at | INTEGER | no |  | internal | false | When the session started, Unix milliseconds UTC. |
| expires_at | INTEGER | no |  | internal | false | When the session expires, Unix milliseconds UTC. |
| revoked_at | INTEGER | yes |  | internal | false | When it was ended early, Unix milliseconds UTC; empty otherwise. |

## ldg_checkpoint

Module: ledger. Classification: internal. Billable: false.

One signed checkpoint of the usage ledger: the tree head at a size, as a C2SP signed note. Immutable, kept forever.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| tree_size | INTEGER | no | PK | internal | false | Number of leaves the checkpoint covers (leaves 0 to tree_size - 1); at least 1. |
| root_hash | BLOB | no |  | internal | false | The RFC 6962 root hash of the first tree_size leaves (32 bytes). |
| note | TEXT | no |  | internal | false | The signed note text verbatim: origin, size, base64 root and the signature line. |
| key_id | TEXT | no |  | internal | false | The hostkeys id of the checkpoint key that signed it; the key stays published after it retires. |
| created_at | INTEGER | no |  | internal | false | When the checkpoint was signed, Unix milliseconds UTC. |

## ldg_hash

Module: ledger. Classification: internal. Billable: false.

The tlog stored hashes of the ledger tree, in tlog.StoredHashIndex order. Immutable, kept forever.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| hash_index | INTEGER | no | PK | internal | false | tlog.StoredHashIndex(level, n) of the tree node. |
| hash | BLOB | no |  | internal | false | The node's 32-byte hash. |

## ldg_leaf

Module: ledger. Classification: internal. Billable: false.

One leaf of the usage ledger: the position of a usage record and the hash of its canonical leaf bytes. Immutable, kept forever.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| leaf_index | INTEGER | no | PK | internal | false | Position in the log, from 0 with no gaps; the tree size after this leaf is leaf_index + 1. |
| usage_id | TEXT | no |  | internal | false | The use_record this leaf commits to. |
| leaf_hash | BLOB | no |  | internal | false | tlog.RecordHash of the canonical leaf bytes (SHA-256 over 0x00 and the bytes, RFC 6962). |
| created_at | INTEGER | no |  | internal | false | When the leaf was appended, Unix milliseconds UTC. |

## ntf_event

Module: events. Classification: internal. Billable: false.

A domain event: something that happened (a request created, a host paused, a quota near its limit), written in the unit of work of the change that caused it.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Event id, UUIDv7. |
| type | TEXT | no |  | internal | false | Event type such as host.paused; a closed set defined in code. |
| occurred_at | INTEGER | no |  | internal | false | When the event happened, Unix milliseconds UTC. |
| actor_principal_id | TEXT | yes |  | internal | false | The principal that caused the event; empty when the system did. |
| target_principal_id | TEXT | yes |  | internal | false | The principal the event is about; empty when none. |
| payload | TEXT | no |  | internal | false | JSON object with ids and counters describing the event; never a prompt, a response or a secret. |

## ops_data_catalog

Module: storage. Classification: internal. Billable: false.

One row per table and per column: what it holds, how sensitive it is and whether it is billable. Rebuilt from the migrations at every startup.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| table_name | TEXT | no | PK | internal | false | Table the row describes. |
| column_name | TEXT | no | PK | internal | false | Column the row describes; empty for the table itself. |
| module | TEXT | no |  | internal | false | Module that owns the table. |
| classification | TEXT | no |  | internal | false | public, internal, personal, sensitive or content (D-44). |
| billable | INTEGER | no |  | internal | false | 1 when the data may be billed for later, otherwise 0. |
| comment | TEXT | no |  | internal | false | Plain-language description. |

## ops_migration_run

Module: storage. Classification: internal. Billable: false.

Log of every attempt to apply a migration, including failed ones.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | INTEGER | no | PK | internal | false | Attempt number. |
| module | TEXT | no |  | internal | false | Name of the module that owns the migration. |
| version | INTEGER | no |  | internal | false | Migration number inside the module. |
| name | TEXT | no |  | internal | false | Short name taken from the migration file name. |
| started_at | INTEGER | no |  | internal | false | When the attempt started, Unix milliseconds UTC. |
| finished_at | INTEGER | no |  | internal | false | When the attempt ended, Unix milliseconds UTC. |
| outcome | TEXT | no |  | internal | false | applied or failed. |
| error | TEXT | yes |  | internal | false | Error text of a failed attempt; empty when it was applied. |

## ops_schema_migrations

Module: storage. Classification: internal. Billable: false.

Which migration of which module has been applied, with the checksum of its file at that time.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| module | TEXT | no | PK | internal | false | Name of the module that owns the migration. |
| version | INTEGER | no | PK | internal | false | Migration number inside the module, increasing. |
| name | TEXT | no |  | internal | false | Short name taken from the migration file name. |
| checksum | TEXT | no |  | internal | false | SHA-256 of the migration file as applied; a changed file stops startup. |
| applied_at | INTEGER | no |  | internal | false | When it was applied, Unix milliseconds UTC. |
| duration_ms | INTEGER | no |  | internal | false | How long applying it took, in milliseconds. |

## sch_job

Module: scheduler. Classification: internal. Billable: false.

One background job: who submitted it, with which Idempotency-Key, its state and times. Holds no prompt and no response; those are in sch_job_payload and sch_job_result.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | false | Job id, UUIDv7. |
| consumer_id | TEXT | no |  | internal | false | The consumer that submitted the job; only this consumer can see it. |
| principal_id | TEXT | no |  | internal | false | The consumer's principal, the target of the job.done event. |
| access_key_id | TEXT | no |  | internal | false | The access key the job was submitted with; the Idempotency-Key is unique per access key. |
| grant_version_id | TEXT | no |  | internal | false | The grant version in force when the job was submitted; the job is metered under it. |
| idempotency_key | TEXT | no |  | internal | false | The client-chosen Idempotency-Key. A resubmission with the same key returns this job, unless it failed with host_restart or payload_minimized. |
| model | TEXT | no |  | internal | false | The model the job asked for. |
| identity | TEXT | no |  | internal | false | JSON copy of the submitting key's identity and limits (ids and numbers only), used to check policy and quota again when the job is admitted. |
| status | TEXT | no |  | internal | false | queued, running, succeeded, failed, cancelled or expired. The last four are terminal and never reopen. |
| failure_reason | TEXT | yes |  | internal | false | Why a failed job failed: host_restart, payload_minimized or an error code such as engine_unavailable; null otherwise. |
| created_at | INTEGER | no |  | internal | false | When the job was queued, Unix milliseconds UTC. |
| not_before | INTEGER | yes |  | internal | false | A queued job is not offered to the scheduler before this time (a policy time window that is closed, or a retry wait); null means at once. Unix milliseconds UTC. |
| deadline_at | INTEGER | yes |  | internal | false | A queued job that has not started by this time expires; null means it never expires. From the consumer's jobs.deadline setting, Unix milliseconds UTC. |
| started_at | INTEGER | yes |  | internal | false | When the scheduler admitted the job and it started running, Unix milliseconds UTC. |
| finished_at | INTEGER | yes |  | internal | false | When the job reached a terminal state, Unix milliseconds UTC. |

## sch_job_payload

Module: scheduler. Classification: content. Billable: false.

A queued job's request content, encrypted with the host data key. Deleted when the job reaches any terminal state. Never synced and left out of backups by default.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| job_id | TEXT | no | PK | internal | false | The job. |
| sealed | BLOB | no |  | content | false | The request (messages, token limit and request properties) sealed with AES-256-GCM under the host data key, bound to the table, the job id and the consumer. |
| created_at | INTEGER | no |  | internal | false | When the payload was stored, Unix milliseconds UTC. |

## sch_job_result

Module: scheduler. Classification: content. Billable: false.

A succeeded job's answer, encrypted with the host data key, served only to the job's consumer. Deleted once a fetch has written it whole to the consumer (D-85) or when it passes expires_at. Never synced and left out of backups by default.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| job_id | TEXT | no | PK | internal | false | The job. |
| sealed | BLOB | no |  | content | false | The chat completion sealed with AES-256-GCM under the host data key, bound to the table, the job id and the consumer. |
| receipt | TEXT | no |  | internal | false | The signed usage receipt (JWS) returned in the LS-Receipt header with the result; holds counts and ids only. |
| created_at | INTEGER | no |  | internal | false | When the result was stored, Unix milliseconds UTC. |
| expires_at | INTEGER | no |  | internal | false | When the unfetched result is deleted: the job finished plus the consumer's jobs.result_ttl setting at that time, Unix milliseconds UTC. |

## syn_change

Module: storage. Classification: internal. Billable: false.

Every change to a synced row, written in the same transaction as the change, waiting to be shipped off the host.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| seq | INTEGER | no | PK | internal | false | Increasing sequence number; the cursor of each sync target points into it. |
| dataset | TEXT | no |  | internal | false | Dataset the changed row belongs to. |
| table_name | TEXT | no |  | internal | false | Table of the changed row. |
| pk | TEXT | no |  | internal | false | Primary key of the changed row. |
| version | INTEGER | no |  | internal | false | Row version after the change. |
| op | TEXT | no |  | internal | false | insert, update or delete. |
| payload | BLOB | yes |  | internal | false | The row as the module chose to record it; never prompts or responses (D-31, D-40). |
| created_at | INTEGER | no |  | internal | false | When the change was captured, Unix milliseconds UTC. |

## use_record

Module: usage. Classification: internal. Billable: true.

One metered call: who, which model, how many tokens, how long. Immutable and kept forever; never holds prompts or responses.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | TEXT | no | PK | internal | true | Usage id (UUID), also in the receipt and the ledger leaf. |
| consumer_id | TEXT | no |  | internal | true | The consumer, copied from the grant at call time. |
| access_key_id | TEXT | no |  | internal | true | The access key that made the call, copied. |
| grant_version_id | TEXT | no |  | internal | true | The grant version the call ran under, copied. |
| job_id | TEXT | yes |  | internal | true | The job, when the call ran as one; null otherwise. |
| operation | TEXT | no |  | internal | true | Surface called: chat.completions or embeddings. |
| model | TEXT | no |  | internal | true | The model that answered. |
| outcome | TEXT | no |  | internal | true | completed, aborted (client disconnected) or failed (engine failed). |
| prompt_tokens | INTEGER | no |  | internal | true | Prompt tokens, from the engine. |
| completion_tokens | INTEGER | no |  | internal | true | Generated tokens, from the engine. |
| total_tokens | INTEGER | no |  | internal | true | Prompt plus generated tokens. |
| latency_ms | INTEGER | no |  | internal | false | Milliseconds from admission to the end of the call. |
| started_at | INTEGER | no |  | internal | true | When the call was admitted, Unix milliseconds UTC. |
| finished_at | INTEGER | no |  | internal | true | When the call ended, Unix milliseconds UTC. |
| receipt_kid | TEXT | no |  | internal | false | Id of the receipt-signing key that signed this call's receipt. |
| receipt_signature | BLOB | no |  | internal | false | The receipt's Ed25519 signature (64 bytes), kept so the receipt can be rebuilt and checked. |
| created_at | INTEGER | no |  | internal | false | When the record was written, Unix milliseconds UTC. |

## use_request_counter

Module: usage. Classification: internal. Billable: true.

Calls admitted for one subject in one fixed quota window, metered or not. Numbers and ids only.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| subject_kind | TEXT | no | PK | internal | true | Whose calls are counted: consumer (all its keys) or access_key. |
| subject_id | TEXT | no | PK | internal | true | The consumer id or access key id, copied. |
| window_ms | INTEGER | no | PK | internal | true | Length of the quota window in milliseconds. |
| window_start_ms | INTEGER | no | PK | internal | true | Start of the fixed window, Unix milliseconds UTC, a whole multiple of the window length. |
| request_count | INTEGER | no |  | internal | true | Calls admitted in the window so far. |

## use_rollup

Module: usage. Classification: internal. Billable: true.

Usage totals for one bucket (minute, hour or day), consumer, access key and model, summed from use_record by the rollup worker. Numbers and ids only.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| granularity | TEXT | no | PK | internal | true | Bucket size: minute, hour or day. |
| bucket_start | INTEGER | no | PK | internal | true | Start of the bucket, Unix milliseconds UTC, aligned to the bucket size. |
| consumer_id | TEXT | no | PK | internal | true | The consumer, copied from the use_record rows. |
| access_key_id | TEXT | no | PK | internal | true | The access key, copied from the use_record rows. |
| model | TEXT | no | PK | internal | true | The model that answered. |
| requests | INTEGER | no |  | internal | true | Metered calls in the bucket, whatever their outcome (completed, aborted or failed). |
| prompt_tokens | INTEGER | no |  | internal | true | Prompt tokens in the bucket. |
| completion_tokens | INTEGER | no |  | internal | true | Generated tokens in the bucket. |
| total_tokens | INTEGER | no |  | internal | true | Prompt plus generated tokens in the bucket. |

## use_rollup_state

Module: usage. Classification: internal. Billable: false.

The single row saying how far the rollup worker has read use_record.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | INTEGER | no | PK | internal | false | Always 1: there is one row. |
| last_rowid | INTEGER | no |  | internal | false | The rowid of the last use_record row already added to use_rollup; 0 before the first. |
