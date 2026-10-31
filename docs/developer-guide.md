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
| `GET /v1/health` | local-server | Online, paused, loaded models, queue depth. Call it to decide quickly whether to fall back |
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
  - `LS-Receipt` (response) carries the signed usage receipt. When streaming, the receipt also arrives in a final event before `[DONE]`, which standard SDKs skip.

### Errors and retries

Errors use the OpenAI error envelope plus a typed `code`. Use `Retry-After` where it is present.

| `code` | What to do |
|---|---|
| `quota_exceeded` | Wait for `Retry-After`, or file a resource request |
| `queue_full` | Retry after `Retry-After` with backoff, or fall back |
| `host_paused` | Fall back now. Queue background work and resend when `/v1/health` says the host is back |
| `model_not_granted` | Check `/v1/capabilities`, or ask for the model |
| `context_too_long` | Shorten the prompt or ask for a larger context limit |
| `engine_unavailable` | Fall back |

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
- **The result is deleted when you fetch it** (or after the job-result time you set, 24 hours by default). Store it yourself.

### Verifying receipts

A receipt is a JWS signed with Ed25519 over your call's usage record (its exact fields are in the spec), plus the ledger `leaf_index` and the tree size that leaf creates.

To verify one:
1. Fetch the host's public keys from `/v1/keys`, and verify the receipt's signature.
2. Later, call `GET /v1/usage/{id}/proof`. It returns the first signed checkpoint that covers your leaf, plus an inclusion proof. Verify both.
3. Keep the checkpoints you see. A consistency proof between an old one and a new one shows that the host only appended to its ledger and never rewrote it.

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

### Commands (once the code scaffold exists)

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
- **Boundaries:** `tools/archcheck` fails the build on imports outside the module map. The map is in [LLD §1](design_docs/lld.md#1-module-map).
- **Contract first:** the API starts from `api/openapi.yaml`. Server interfaces are generated with ogen into `internal/api/` and never edited by hand. Change the spec, then run `go generate ./internal/api/...`.

### Rules contributors follow

- **Settings, not constants:** every time, retention, TTL, retry and threshold value is a runtime setting with an owner, never a constant.
- **Privacy:** prompts and responses are not logged or stored by default; only background-job content is kept, encrypted, until the job ends or its result is fetched. Redaction is enforced by types (`slog.LogValuer`).
- **Fuzzing:** every new parser of untrusted input gets a fuzz target with a seed corpus.
- **Shared contract tests:** a new adapter must pass its port's shared contract test suite.
- **Darwin binaries:** never strip or alter a built darwin binary.
- **Words:** use the [glossary](glossary.md)'s terms, one word for one meaning.
