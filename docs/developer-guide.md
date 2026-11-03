# local-server developer guide

> **Status: planned.** This guide describes the Phase 1 API as designed. Once it exists, the reference is the OpenAPI 3.1 spec at `api/openapi.yaml`, and any example here that disagrees with the spec is wrong. Design background: [HLD](design_docs/hld.md) · [LLD](design_docs/lld.md).

This guide is for two readers:
- developers of **client apps** that call a host's local-server;
- contributors who build local-server itself.

## Part 1 — calling a host from your app

### The short version

local-server speaks the **OpenAI API**. Any OpenAI SDK works if you change the base URL and the API key.

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://<host-machine>.<tailnet>.ts.net/v1",  # from the host
    api_key="ls_<prefix>_<secret>",                         # access key, shown once to the host
)
reply = client.chat.completions.create(
    model="llama3.1:8b",                                     # a model your grant allows
    messages=[{"role": "user", "content": "Hello"}],
)
print(reply.choices[0].message.content)
```

```bash
curl https://<host-machine>.<tailnet>.ts.net/v1/chat/completions \
  -H "Authorization: Bearer ls_<prefix>_<secret>" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.1:8b","messages":[{"role":"user","content":"Hello"}]}'
```

The base URL's host name and port are the host's tailnet name and the port they configure. Your app must be on the host's tailnet (as a device the host shares with, or as an invited user), unless the host turns on public mode.

### Endpoints

| Endpoint | Standard? | Use |
|---|---|---|
| `POST /v1/chat/completions` | OpenAI | Chat, including streaming with `stream_options.include_usage` |
| `POST /v1/embeddings` | OpenAI | Embeddings |
| `GET /v1/models` | OpenAI | Only the models your grant allows |
| `GET /v1/health` | local-server | Online; sharing, paused or quiet; whether the engine answers; the loaded models your grant allows; queue depth. Call it to decide quickly whether to fall back |
| `GET /v1/capabilities` | local-server | Your models, context limits, max output and remaining quota |
| `POST /v1/jobs`, `GET /v1/jobs/{id}`, `GET /v1/jobs/{id}/result` | local-server | Background work that should outlive a request |
| `/v1/resource-requests` | local-server | Ask the host for more, with comments |
| `GET /v1/usage` | local-server | Your usage over time |
| `GET /v1/keys`, `GET /v1/ledger/checkpoint`, `GET /v1/usage/{id}/proof` | local-server | Verify receipts |
| Notification preferences, webhook endpoints | local-server | Get events pushed to you |

### Rules that differ from OpenAI

- **Unsupported fields are rejected,** never silently ignored. Fields such as `n`, `logit_bias` and `tool_choice` return a typed error, so your app knows they had no effect.
- **`max_tokens` is capped** by your grant.
- **Context limits are enforced.** By default, an over-long prompt fails with `context_too_long` instead of being silently cut. The host can choose truncation for your grant instead.
- **Extensions live in headers,** so the body stays standard:
  - `LS-Request-Properties` (request) tags the call with properties the host's policy rules understand. For example, a background call can be allowed only in background-work hours. The header's format is defined in the spec.
  - `LS-Receipt` (response) carries the signed usage receipt of a non-streamed call. A streamed response has no `LS-Receipt` header: its receipt arrives instead in an SSE comment line `: ls-receipt <JWS>` after the last chunk and before `data: [DONE]`. Standard SDKs skip comment lines, so read the raw event stream if you want to keep a streamed call's receipt.

### Errors and retries

Errors use the OpenAI error envelope plus a typed `code`. Use `Retry-After` where it is present.

| `code` | What to do |
|---|---|
| `quota_exceeded` | Wait for `Retry-After`, or file a resource request |
| `queue_full` | Retry after `Retry-After` with backoff, or fall back. A background call can also get it after waiting, when interactive calls need its place. |
| `host_paused` | Fall back now. Queue background work and resend when `/v1/health` says the host is back |
| `model_not_granted` | Check `/v1/capabilities`, or ask for the model |
| `context_too_long` | Shorten the prompt or ask for a larger context limit |
| `engine_unavailable` | Fall back |
| `over_budget` | The model and context can never fit this host's memory budget; use a smaller model or shorter context, retrying the same request does not help |
| `policy_denied` | A policy rule refuses the request, or it carries a property your grant does not allow; `rule_id`, `rule_name` and `properties` say which. Change the request or ask the host, because retrying the same request does not help |
| `outside_time_window` | A policy rule allows this request only in its time windows; retry after `Retry-After`, when the rule's next window opens |
| `invalid_request_properties` | Fix the `LS-Request-Properties` header; it is malformed or over the host's limits |
| `unsupported_parameter` | Remove the field named in `param` |
| `invalid_api_key` | The access key is missing, unknown, expired or revoked; get a valid key from the host |
| `rate_limit_exceeded` | Slow down and retry after `Retry-After`; after repeated failed key checks this is a lockout |

A good client:
- checks `/v1/health` before falling back;
- retries with exponential backoff and jitter, honouring `Retry-After`;
- keeps its own retry queue for background work while the host is offline.

### Background jobs and idempotency

```bash
curl -X POST https://<host>/v1/jobs \
  -H "Authorization: Bearer ls_<prefix>_<secret>" \
  -H "Idempotency-Key: 7f6c1d0e-..." \
  -d '{ ...the same body as /v1/chat/completions... }'
# → 202 {"id": "...", "status": "queued"}
```

- **Duplicates:** sending the same `Idempotency-Key` again returns the existing job, so a retry after a timeout never runs or bills twice. The exception is a job that ended `failed` with reason `host_restart` (the host restarted while it ran) or `payload_minimized` (the host restored a backup that left job content out). Send the same key again to start a new attempt.
- **Getting the result:** poll `GET /v1/jobs/{id}`, or register a webhook for `job.done`. `job.done` is sent when a job succeeds. A job that ends `failed`, `cancelled` or `expired` shows its status and reason on `GET /v1/jobs/{id}`.
- **The result is deleted once a fetch has delivered it whole** (or after the job-result time you set, 24 hours by default). If the connection drops before the body is fully written, fetch again until the time runs out. Store it yourself.

### Verifying receipts

A receipt is a JWS signed with Ed25519 over your call's usage record (its exact fields are in the spec), plus the ledger `leaf_index` and the tree size that leaf creates.

To verify one:
1. Fetch `/v1/keys`. The identity key is the key whose `kid` equals the receipt's `host_kid`, which is the host ID. Keep the host ID from your first contact and refuse a key set whose identity key differs.
2. Find the receipt's key by the JWS header `kid`. Check its `ls_endorsement`: the `signature` must verify, under the identity key, over the base64url-decoded `certificate`. The certificate must name this key: the host ID, the key's `ls_role`, its `x`, and its `ls_created_at` as Unix milliseconds.
3. Verify the receipt's signature with that key.
4. Later, call `GET /v1/usage/{id}/proof`. It returns the first signed checkpoint that covers your leaf, plus an inclusion proof. Verify both.
5. Keep the checkpoints you see. A consistency proof between an old one and a new one shows that the host only appended to its ledger and never rewrote it.

A receipt marked `unmetered` means the host answered but could not record the call. It is signed evidence that the call can't be billed later.

### Webhooks

Deliveries follow [Standard Webhooks](https://www.standardwebhooks.com/): the `webhook-id`, `webhook-timestamp` and `webhook-signature` headers (HMAC-SHA256, `v1`). Any Standard Webhooks library can verify them.

- **De-duplicate by `webhook-id`.** Delivery is at least once.
- **Answer `410 Gone`** to stop deliveries to your endpoint.
- **No content:** payloads never carry prompt or answer content.
- **Private addresses:** a URL that resolves to a private (RFC 1918, IPv6 ULA) or tailnet (100.64.0.0/10) address gets no deliveries unless the host has allowed private targets. Loopback, link-local and the host's own addresses never do. The address is checked when each delivery is sent, and redirects are not followed.

### Versioning

- The major version is in the path (`/v1`). Within a major version, changes are additive only.
- Deprecations are announced with the `Deprecation` and `Sunset` headers.

## Part 2 — building local-server

### Prerequisites

- Go (the version pinned in `go.mod`).
- No C toolchain is needed: builds are pure Go (`CGO_ENABLED=0`).
- To run the race detector locally, you need a C toolchain (Xcode command-line tools on macOS).

### Commands

```bash
CGO_ENABLED=0 go build ./...          # build
go vet ./... && go test ./...         # test (runs fuzz seed corpora too)
go run ./tools/archcheck              # module boundaries
go tool govulncheck ./...             # known vulnerabilities
CGO_ENABLED=1 go test -race ./...     # race detector
go test -fuzz=FuzzXxx ./internal/auth/  # fuzz one target
```

### How the code is organised

- **Layout:** core modules live in `internal/<module>/`. Each owns its tables and depends only on ports (interfaces). Adapters (Ollama, Tailscale, SQLite, S3, Postgres, OS integrations) implement the ports.
- **Boundaries:** `tools/archcheck` fails the build on forbidden imports: an adapter imported outside `cmd/`, a module import cycle, a driver used outside its module, or `internal/` importing `cmd/` or `tools/`. The map is in [LLD §1](design_docs/lld.md#1-module-map).
- **Contract first:** the API starts from `api/openapi.yaml`. Server interfaces are generated with ogen into `internal/api/` and never edited by hand. Change the spec, then run `go generate ./internal/api/...`.

### Rules contributors follow

- **Settings, not constants:** every time, retention, TTL, retry and threshold value is a runtime setting with an owner, never a constant.
- **Privacy:** prompts and responses are not logged or stored by default; only background-job content is kept, encrypted, until the job ends or its result is fetched. Redaction is enforced by types (`slog.LogValuer`).
- **Fuzzing:** every new parser of untrusted input gets a fuzz target with a seed corpus.
- **Shared contract tests:** a new adapter must pass its port's shared contract test suite.
- **Darwin binaries:** never strip or alter a built darwin binary.
- **Words:** use the [glossary](glossary.md)'s terms, one word for one meaning.
