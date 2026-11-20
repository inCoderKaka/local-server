# local-server developer guide

> **Status: describes the built Phase 1 API.** The reference is the OpenAPI 3.1 spec at [`api/openapi.yaml`](../api/openapi.yaml), and any example here that disagrees with the spec is wrong. Everything in this guide that is not marked *planned* is built. Features marked *planned (phase 2)* are not built and have no date. Terms are defined in the [glossary](glossary.md). Design background: [HLD](design_docs/hld.md) · [LLD](design_docs/lld.md).

This guide is for two readers:
- developers of **client apps** that call a host's local-server (Part 1);
- contributors who build local-server itself (Part 2).

## Part 1 — calling a host from your app

### The short version

local-server speaks the **OpenAI API**. Any OpenAI SDK works if you change the base URL and the API key.

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://<host-machine>.<tailnet>.ts.net:8484/v1",  # from the host
    api_key="ls_<prefix>_<secret>",                              # access key, shown once to the host
)
reply = client.chat.completions.create(
    model="llama3.1:8b",                                          # a model your grant allows
    messages=[{"role": "user", "content": "Hello"}],
)
print(reply.choices[0].message.content)
```

```bash
curl https://<host-machine>.<tailnet>.ts.net:8484/v1/chat/completions \
  -H "Authorization: Bearer ls_<prefix>_<secret>" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3.1:8b","messages":[{"role":"user","content":"Hello"}]}'
```

- **The address:** the host gives you the base URL and the **access key**. The host's tailnet name is the host name, and the API port is 8484 unless the host changed the setting `serve.tailnet_consumer_port`. The consumer web page and the admin page are on other ports and are not for apps. On the host machine itself, the API is at `http://127.0.0.1:8484/v1`.
- **The network:** your app must run on a device on the host's tailnet (a device the host shares the machine with, or a user the host invited). A server hosted elsewhere must join the tailnet as a node a user owns; see [Let people and apps reach you](user-guide.md#let-people-and-apps-reach-you). Public mode, which needs no Tailscale, is *planned (phase 2)*. The host can also tie a key to one device.
- **Browsers:** a web page cannot call the API directly, because the API refuses cross-site calls and answers no CORS headers. Call it from a server.
- **Credentials:** every `/v1` call takes the access key as `Authorization: Bearer ls_<prefix>_<secret>`. A session cookie never works on the API. Every response carries `X-Request-Id`; quote it when you report a failure, and send your own (letters, digits, `-` and `_`, at most 64 characters) to have it kept.

### Endpoints

All of these take an access key. The spec lists every request and response field.

| Endpoint | Standard? | Use |
|---|---|---|
| `POST /v1/chat/completions` | OpenAI | Chat, including streaming with `stream_options.include_usage` |
| `POST /v1/embeddings` | OpenAI | Embeddings |
| `GET /v1/models` | OpenAI | Only the models your grant allows |
| `GET /v1/health` | local-server | Host status: `sharing_state` (`sharing`, `paused` or `quiet`), `sharing_resumes_at` (when quiet hours end), `engine_state` (`available` or `unavailable`), `loaded_models` (loaded models your grant allows), `queue_depth` (`interactive` and `background`) and `checked_at`. A response at all means the host is online. Call it to decide quickly whether to fall back |
| `GET /v1/capabilities` | local-server | What you may use: `models` (each with its `context_limit` and `max_output_tokens`), `concurrency`, `on_context_overflow`, `allowed_request_properties` (`null` means no restriction), `quotas` with what is left, `rate_limit`, `expires_at`, after your key's narrowing. `benchmark` is reserved and always `null` |
| `POST /v1/jobs`, `GET /v1/jobs/{job_id}`, `POST /v1/jobs/{job_id}/cancel`, `GET /v1/jobs/{job_id}/result` | local-server | Background work that outlives a request |
| `GET /v1/resource-requests`, `POST /v1/resource-requests`, `GET /v1/resource-requests/{resource_request_id}`, `POST /v1/resource-requests/{resource_request_id}/comments`, `POST /v1/resource-requests/{resource_request_id}/withdraw` | local-server | Ask the host for more, with comments |
| `GET /v1/usage` | local-server | Your usage over time |
| `GET /v1/keys`, `GET /v1/ledger/checkpoint`, `GET /v1/usage/{usage_id}/proof` | local-server | Verify receipts |
| `GET /v1/me`, `GET /v1/me/limits`, `GET /v1/me/access-keys`, `GET /v1/me/settings`, `PATCH /v1/me/settings` | local-server | Your account (including its `billing_mode`), your current grant before your key narrows it, your keys by prefix (never a secret), and the settings you own |
| `/v1/notification-preferences`, `/v1/webhook-endpoints` | local-server | Get events pushed to you. *Planned (phase 2)*: they answer `501` today (see [Webhooks and notifications](#webhooks-and-notifications-planned-phase-2)) |

The sign-up, login, password, passkey and session routes in the spec (`/v1/invite-redemptions/*`, `/v1/session*`, `/v1/me/password`, `/v1/me/passkeys/*`) belong to the consumer web page and its session cookie. A client app never calls them, and on the API they answer `401`.

### Rules that differ from OpenAI

- **Unsupported fields are rejected,** never silently ignored. A field the spec does not list (for example `n`, `logit_bias`, `tool_choice`, `user`, `logprobs`) returns `400` with code `unsupported_parameter` and `param` naming it, so your app knows it had no effect. So do fields the spec lists but the host's engine cannot carry yet: `tools`, `frequency_penalty`, `presence_penalty`, a `response_format` other than `text`, image parts, tool calls and tool messages in `messages`, and `dimensions` for embeddings. `temperature`, `top_p`, `seed` and `stop` are passed to the engine. `stream_options` needs `stream: true`, and you send `max_tokens` or `max_completion_tokens`, not both.
- **`max_tokens` is capped** by your grant's output limit. A request that sets none gets the host's default (`gateway.default_max_tokens`, 1024), and where the grant sets no limit the host's ceiling applies (`gateway.max_tokens_ceiling`, 8192). A request that asks for more is capped, not refused.
- **Context limits are enforced.** By default, an over-long prompt fails with `context_too_long` instead of being silently cut. The host can choose truncation for your grant instead (`on_context_overflow` in `/v1/capabilities`). Where the grant sets no context limit, the host's default applies (`gateway.default_context_tokens`, 4096). One embeddings request may carry at most `gateway.max_embedding_inputs` texts (128 by default), and a request body over `limits.max_body_bytes` (8 MiB by default) fails with `413`.
- **Streaming:** with `stream: true` the answer is server-sent events ending in `data: [DONE]`. With `stream_options.include_usage` the last chunk before `[DONE]` carries `usage` and an empty `choices`. An error after the stream has started arrives as a `data:` line holding the usual error envelope.
- **Extensions live in headers,** so the body stays standard:
  - `LS-Request-Properties` (request) tags the call with properties the host's policy rules understand, as an RFC 9651 Structured Field Dictionary such as `background, purpose=summarize`. For example, a background call can be allowed only in background-work hours. A property your grant does not allow is refused (`policy_denied`). The format is defined in the spec.
  - `LS-Receipt` (response) carries the signed usage receipt of a non-streamed call, and of a job's result. A streamed response has no `LS-Receipt` header: its receipt arrives instead in an SSE comment line `: ls-receipt <JWS>` after the last chunk and before `data: [DONE]`. Standard SDKs skip comment lines, so read the raw event stream if you want to keep a streamed call's receipt. A stream your client abandons has no receipt on the stream, but its tokens are still recorded.
  - `Idempotency-Key` (request, required on `POST /v1/jobs`) makes a job submission safe to retry (see [Background jobs](#background-jobs-and-idempotency)).
  - `Retry-After` (response) is in seconds, where a retry makes sense.
  - `Deprecation` and `Sunset` (response) announce a deprecation (see [Versioning](#versioning)).

### Quotas

A quota is a list of entries `{metric, limit, window_seconds}`: `metric` is `tokens` or `requests`, and an entry caps that metric in each fixed window of `window_seconds`. A grant or a key has at most one entry per metric and window, and every entry is enforced on its own. Windows are fixed and start at whole multiples of their length counted from 0001-01-01 00:00 UTC, so a length that divides 24 hours starts a window at every midnight UTC. A call counts in the window that holds the moment the host admitted it, so a call that runs across a window's end is counted once. The request metric counts every admitted call, metered or not.

- **The older `quota` object** (`{tokens, requests}`) is deprecated. It stands for entries whose window is the host's default window (`limits.quota_window`, 24 hours by default), and the host still returns it when such entries exist. When you send both `quota` and `quotas`, `quotas` wins and the answer's `warnings` says the other was ignored.
- **Choosing a window:** a window is whole seconds within the host's `limits.quota_window_min` and `limits.quota_window_max` (1 minute and 30 days by default). Today the host sets the windows of your grant and keys. A resource request that asks for a window other than the host's default is honoured only when the host has set the consumer's billing mode to `prepaid` or `postpaid`. Choosing the windows of your own keys comes with the route that lets a consumer issue its own key, which is *planned (phase 2)*: it will allow it for those two billing modes and answer `403` to a consumer with billing mode `none`. No balances, invoices or prices exist in this version.
- **Reading it:** `GET /v1/capabilities` returns one quota status for each scope (`grant` or `access_key`) and window, with the tokens and requests of that window together and `resets_at`, the end of the current window.
- **Rate limit:** each access key also has a request allowance that refills over time (`rate_limit` in `/v1/capabilities`; 60 requests, refilled in a minute by default).

### Errors and retries

Errors use the OpenAI error envelope `{"error": {"message", "type", "param", "code"}}` plus a typed `code`. Use `Retry-After` where it is present. Messages never carry prompt or answer content.

| `code` | HTTP status | What to do |
|---|---|---|
| `quota_exceeded` | 429 | A quota entry of your grant or key is used up for its current window. Wait for `Retry-After`, which is the time to that window's end, or file a resource request |
| `queue_full` | 429 | Retry after `Retry-After` with backoff, or fall back. A background call can also get it after waiting, when interactive calls need its place. |
| `host_paused` | 503 | Fall back now. Queue background work and resend when `/v1/health` says the host is back. `Retry-After` is there when the end of quiet hours is known |
| `model_not_granted` | 403 | Check `/v1/capabilities`, or ask for the model. A consumer the host has not given a grant yet gets this for every model |
| `context_too_long` | 400 | Shorten the prompt or ask for a larger context limit |
| `engine_unavailable` | 503 | Fall back |
| `over_budget` | 422 | The model and context can never fit this host's memory budget; use a smaller model or shorter context, retrying the same request does not help |
| `policy_denied` | 403 | A policy rule refuses the request, or it carries a property your grant does not allow; `rule_id`, `rule_name` and `properties` say which. Change the request or ask the host, because retrying the same request does not help |
| `outside_time_window` | 429 | A policy rule allows this request only in its time windows; retry after `Retry-After`, when the rule's next window opens |
| `invalid_request_properties` | 400 | Fix the `LS-Request-Properties` header; it is malformed or over the host's limits |
| `unsupported_parameter` | 400 | Remove the field named in `param` |
| `invalid_api_key` | 401 | The access key is missing, unknown, expired or revoked, its consumer is suspended or its grant has expired, or it is used from a device it is not tied to; get a valid key from the host |
| `rate_limit_exceeded` | 429 | Slow down and retry after `Retry-After`. One allowance per access key covers every `/v1` call made with the key, status reads such as `/v1/health`, `/v1/models` and `/v1/capabilities` included, so polling them spends it too. After repeated failed key checks this is a lockout |

Other failures use their HTTP status with `code: null`: `400` for a bad field (`param` names it), `404` for an unknown model, job, request or usage record (or one that is not yours), `409` for a state that does not allow the call, `410` for a job result that is gone, `413` for a body over the host's limit, `501` for a route that is not built yet, `502` and `504` when the engine fails or does not answer in time.

A good client:
- checks `/v1/health` before falling back;
- retries with exponential backoff and jitter, honouring `Retry-After`;
- keeps its own retry queue for background work while the host is offline.

### Background jobs and idempotency

A job is a chat completion that runs when the host admits it, as background work. The body is the one of `/v1/chat/completions`, except that `stream` must be absent or false.

```bash
curl -X POST https://<host-machine>.<tailnet>.ts.net:8484/v1/jobs \
  -H "Authorization: Bearer ls_<prefix>_<secret>" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 7f6c1d0e-3b1a-4c2e-9f7d-2a1b0c9d8e7f" \
  -d '{ ...the same body as /v1/chat/completions... }'
# → 202 {"id": "...", "status": "queued", "model": "...", "idempotency_key": "...",
#        "created_at": "...", "result_available": false, ...}
```

- **Duplicates:** sending the same `Idempotency-Key` again from the same access key returns the existing job with `200`, so a retry after a timeout never runs or bills twice. The exception is a job that ended `failed` with reason `host_restart` (the host restarted while it ran) or `payload_minimized` (the host restored a backup that left job content out). Send the same key again to start a new attempt (`202`). Any other failed job keeps its key: use a new key to try again.
- **Status:** a job is `queued`, `running`, `succeeded`, `failed`, `cancelled` or `expired`; the last four are final. A job that ended `failed`, `cancelled` or `expired` shows its reason on `GET /v1/jobs/{job_id}` (`failure_reason`). A job still queued when its deadline passes expires; the deadline is `jobs.deadline`, a setting you own, 24 hours by default. A policy time window that is closed does not refuse a job: it waits queued until the window opens.
- **Getting the result:** poll `GET /v1/jobs/{job_id}` until `status` is `succeeded` (`result_available` is then `true`), then call `GET /v1/jobs/{job_id}/result`, which returns the chat completion and its receipt in `LS-Receipt`. Asking early returns `409`. Poll with backoff, because every call spends your key's request allowance. Getting a result pushed to you by webhook is *planned (phase 2)*.
- **Cancelling:** `POST /v1/jobs/{job_id}/cancel` cancels a queued or running job. The tokens a running job already used are recorded. A job that has already ended is returned unchanged with `409`.
- **The result is deleted once the host has written it whole to a fetch's connection** (or after the job-result time, `jobs.result_ttl`, a setting you own, 24 hours by default). If the connection drops before the host has written the whole body, fetch again until the time runs out. The host cannot see whether the last bytes reached you, so store the result as soon as you have it. A result that is gone answers `410`. Change the setting with `PATCH /v1/me/settings` and the body `{"values": {"jobs.result_ttl": "1h"}}`.
- **Content at rest:** a queued job's content and its result are the only prompt or answer content local-server keeps, encrypted. The job's content is deleted when the job ends, and its result as above.

### Asking for more

When your grant is not enough (another model, a bigger context, a higher quota, or access at all), send a **resource request**. The host reads it, can comment, and approves or rejects it.

```bash
curl -X POST https://<host-machine>.<tailnet>.ts.net:8484/v1/resource-requests \
  -H "Authorization: Bearer ls_<prefix>_<secret>" \
  -H "Content-Type: application/json" \
  -d '{"message": "Our nightly summaries need a bigger context.",
       "ask": {"models": ["llama3.1:8b"], "context_limit": 8192,
               "quotas": [{"metric": "tokens", "limit": 500000, "window_seconds": 86400}]}}'
# → 201 {"id": "...", "status": "open", "message": "...", "ask": {...},
#        "created_at": "...", "expires_at": "...", ...}
```

- **The ask** is optional and every field in it is optional: `models`, `quotas` (the deprecated `quota` object also works), `context_limit`, `max_output_tokens`, `concurrency` and `request_properties`. The host decides the limits that result, and may change what you asked for. A field of `ask` the host does not know is ignored and named in the answer's `warnings`.
- **States:** `open`, then one of `approved`, `rejected`, `withdrawn` or `expired`. An open request expires after the host's request time (`requests.ttl`, 14 days by default), and one consumer may have a limited number open at once (5 by default); one more answers `409`. On approval, `resulting_grant_version_id` names your new grant version and your limits change at once.
- **Following it:** `GET /v1/resource-requests/{resource_request_id}` returns the request with its comments, oldest first. `GET /v1/resource-requests` lists yours, newest first, without comments; filter with `status` and page with `cursor` and `limit`.
- **Talking:** `POST /v1/resource-requests/{resource_request_id}/comments` with `{"body": "..."}` adds a comment to an open request and returns `201` with the request. `POST /v1/resource-requests/{resource_request_id}/withdraw` withdraws an open request and returns `200`. Both answer `409` once the request has ended. The host's replies appear in the comments; notifications of replies are *planned (phase 2)*, so poll.

### Reading your usage

```bash
curl "https://<host-machine>.<tailnet>.ts.net:8484/v1/usage?granularity=hour&from=2026-01-15T00:00:00Z&to=2026-01-16T00:00:00Z" \
  -H "Authorization: Bearer ls_<prefix>_<secret>"
# → 200 {"data": [{"bucket_start": "2026-01-15T10:00:00Z", "granularity": "hour",
#                  "consumer_id": "...", "access_key_id": "...", "model": "llama3.1:8b",
#                  "requests": 12, "prompt_tokens": 1200, "completion_tokens": 600,
#                  "total_tokens": 1800}],
#        "next_cursor": null}
```

- **Parameters:** `granularity` is required (`minute`, `hour` or `day`). `from` (inclusive) and `to` (exclusive) are RFC 3339 times that select whole buckets by `bucket_start`; `model` and `access_key_id` narrow the answer; `cursor` (the previous page's `next_cursor`) and `limit` page through it.
- **What you get:** one row per bucket, access key and model, for all your keys, oldest bucket first. Buckets are aligned to UTC, and a bucket with no calls has no row.
- **Retention and delay:** the host keeps minute totals for 7 days and hour totals for 90 days by default, and day totals forever; an older range returns no rows at that granularity, so ask for a coarser one. A call shows after the next rollup pass, up to a minute by default.
- For how much of each quota window is left, use `GET /v1/capabilities`.

### Verifying receipts

A receipt is a JWS (protected header `{"alg": "Ed25519", "kid": ..., "typ": "ls-receipt+jws"}`) signed with Ed25519 over your call's usage record (its exact fields are in the spec), plus the ledger `leaf_index` and the tree size that leaf creates. local-server does not ship a verifier; these are the steps to write one.

To verify one:
1. Fetch `/v1/keys`. The identity key is the key whose `kid` equals the receipt's `host_kid`, which is the host ID. Keep the host ID from your first contact and refuse a key set whose identity key differs.
2. Find the receipt's key by the JWS header `kid`. Check its `ls_endorsement`: the `signature` must verify, under the identity key, over the base64url-decoded `certificate`. The certificate must name this key: the host ID, the key's `ls_role`, its `x`, and its `ls_created_at` as Unix milliseconds.
3. Verify the receipt's signature with that key.
4. Later, call `GET /v1/usage/{usage_id}/proof`, with the `usage_id` of the receipt. It returns the first signed checkpoint that covers your leaf (`checkpoint`, `tree_size`), the `leaf_hash` and an inclusion proof (`inclusion_proof`). Until a checkpoint covers the leaf (they are made in the background), it answers `409` with `Retry-After`. Verify:
   - **the leaf:** rebuild the leaf from the receipt's values as the RFC 8785 canonical JSON of `{"access_key_id", "completion_tokens", "consumer_id", "finished_at", "grant_version_id", "job_id" (or null), "model", "operation", "outcome", "prompt_tokens", "started_at", "total_tokens", "usage_id", "v": 1}`, with keys in that order and times as written in the receipt. Its RFC 6962 leaf hash must equal `leaf_hash`;
   - **the checkpoint:** a C2SP signed note whose signature verifies with the checkpoint key's `ls_note_verifier_key` (that key's endorsement is checked as in step 2);
   - **the inclusion:** the proof must connect `leaf_hash` at `leaf_index` to the checkpoint's root hash for its tree size (RFC 6962).
5. Keep the checkpoints you see. `GET /v1/ledger/checkpoint?from_tree_size=<old size>` returns the latest checkpoint and a consistency proof from the old tree size, which shows that the host only appended to its ledger and never rewrote it.

A receipt marked `unmetered` means the host answered but could not record the call. It is signed evidence that the call can't be billed later. It has no `leaf_index` or `tree_size`, and `/v1/usage/{usage_id}/proof` answers `404` for it.

A call you abandon (you disconnect, or cancel a job, while the engine runs) is recorded with outcome `aborted`. It bills the completion tokens produced so far plus an estimated prompt count, because the engine reports its exact prompt count only on its last chunk. The estimate is the prompt's size in bytes divided by the host's `gateway.chars_per_token` setting (4 by default), rounded up, never above the model's context size, and the host works it out in memory without storing the prompt. The receipt of such a call has `prompt_tokens_estimated: true`; the field is absent when `prompt_tokens` is the engine's count. The marker is not part of the ledger leaf, so the leaf hash and proofs are unchanged. A streamed call that fails midway is recorded with outcome `failed` and the tokens received so far. A call that ends in an engine error or timeout before it produced anything writes no record.

### Webhooks and notifications (*planned (phase 2)*)

Notifications are not built. `/v1/notification-preferences` (`GET`, `PUT`) and `/v1/webhook-endpoints` (`GET`, `POST`, and `GET`, `PATCH`, `DELETE` on `/{webhook_endpoint_id}`) are in the spec, but today they answer `501 Not Implemented` with the usual envelope (`"message": "This endpoint is not available on this host yet."`, `"type": "server_error"`, `"code": null`), after the access key has been checked. Read results by polling: `GET /v1/jobs/{job_id}` for a job and `GET /v1/resource-requests/{resource_request_id}` for a resource request.

The planned design, as the spec describes it:
- Deliveries follow [Standard Webhooks](https://www.standardwebhooks.com/): the `webhook-id`, `webhook-timestamp` and `webhook-signature` headers (HMAC-SHA256, `v1`). Any Standard Webhooks library can verify them.
- Delivery is at least once, so de-duplicate by `webhook-id`. Answering `410 Gone` stops deliveries to the endpoint.
- Payloads never carry prompt or answer content.
- A URL that resolves to a private (RFC 1918, IPv6 ULA) or tailnet (100.64.0.0/10) address gets no deliveries unless the host has allowed private targets. Loopback, link-local and the host's own addresses never do. The address is checked when each delivery is sent, and redirects are not followed.

### The learning-portal integration (*planned (phase 2)*)

An integration for client apps that tune their calls closely, the interactive learning portal first, is designed but not built. It adds engine options that the OpenAI API lacks, sent in one namespaced object in the chat request body; a way to discover which options and limits apply to you per model; a response header that reports the options a call actually ran with, after the host's defaults, forced values and caps; usage broken down by task; and a small TypeScript client library. None of it exists in this version, and the spec has no fields for it yet. Until it does, the OpenAI fields above are all a chat request can carry, and `/v1/capabilities` is the only discovery.

### Versioning

- The major version is in the path (`/v1` for the consumer API, `/admin/v1` for the admin API, which only the host uses and which is not served to apps). Within a major version, changes are additive only: a release adds optional request fields, response fields and endpoints, and never removes, renames or tightens one that exists. A client written against an older release keeps working, and your client should ignore a response field it does not know.
- Deprecations are announced with the `Deprecation` and `Sunset` headers. No endpoint is deprecated in this version; the `quota` object (see [Quotas](#quotas)) is the one deprecated field.
- **Unknown request fields differ by API.** The admin API's consumer, access key, invite and grant request bodies are lenient, so a newer client keeps working against an older host: the host ignores a field it does not know, and the answer's `warnings` names the field by its path, for example `narrowing.quotas.0.bogus is not a known field and was ignored`. It never repeats the value. A field name that is empty, unprintable or longer than 64 bytes is not repeated either; the warning then says only that such a field was ignored. The answers to a create, an issue or a change of a consumer, an access key, an invite or a grant carry `warnings`, and the answers that read them back carry none. Check `warnings` after a write, because an ignored field has had no effect. The `ask` of a resource request created on the consumer API is lenient the same way. The OpenAI-compatible chat and embedding bodies of the consumer API stay strict, as described above: an unsupported OpenAI field is rejected with a typed error, never ignored.

## Part 2 — building local-server

### Prerequisites

- Go (the version pinned in `go.mod`).
- No C toolchain is needed: builds are pure Go (`CGO_ENABLED=0`).
- To run the race detector locally, you need a C toolchain (Xcode command-line tools on macOS).
- To build the admin page and the consumer web page, Node.js 24 LTS (`web/.nvmrc`) with npm. A plain `go build` needs no npm; it embeds a one-page placeholder for each page.

### Commands

```bash
CGO_ENABLED=0 go build ./...                           # build
go vet ./... && go test ./...                          # test (runs fuzz seed corpora too)
go run ./tools/archcheck                               # module boundaries
go tool govulncheck ./...                              # known vulnerabilities
go tool vacuum lint --fail-severity error api/openapi.yaml   # lint the spec
go generate ./internal/api/...                         # regenerate the server interfaces from the spec
CGO_ENABLED=1 go test -race ./...                      # race detector
go test -run '^$' -fuzz '^FuzzParseHeader$' ./internal/auth/  # fuzz one target (one package at a time)
make build                                             # both web pages, then bin/local-server
make web-admin-test web-consumer-test                  # web page unit checks, type checks and builds
go run ./cmd/local-server serve --engine fake          # run without Ollama, with the fake engine
```

The same checks run in CI on every change: build, vet, test, archcheck and govulncheck on Linux, macOS and Windows, the spec lint, a check that regenerating the server interfaces leaves `internal/api/` unchanged, the race detector on Linux and macOS, and the web page tests. A scheduled job fuzzes every `Fuzz` target.

### How the code is organised

- **Layout:** core modules live in `internal/<module>/`. Each owns its tables and depends only on ports (interfaces). Adapters (the Ollama and fake engines, Tailscale, the folder and S3 backup targets, OS integrations) implement the ports, and only `cmd/local-server` wires them to the ports and binds the listeners.
- **Boundaries:** `tools/archcheck` fails the build on forbidden imports: an adapter imported outside `cmd/` (or its own directory), a module import cycle, a driver used outside its module, or `internal/` importing `cmd/` or `tools/`. It also fails when a package under `internal/` belongs to no module or a module has no architecture note in [`docs/modules/`](modules/). The map is in [LLD §1](design_docs/lld.md#1-module-map).
- **Contract first:** the API starts from `api/openapi.yaml`. Server interfaces are generated with ogen into `internal/api/` and never edited by hand. Change the spec, then run `go generate ./internal/api/...`.

### Rules contributors follow

- **Settings, not constants:** every time, retention, TTL, retry and threshold value is a runtime setting with an owner, never a constant.
- **Privacy:** prompts and responses are not logged or stored by default; only background-job content is kept, encrypted, until the job ends or its result is fetched. Redaction is enforced by types (`slog.LogValuer`).
- **Fuzzing:** every new parser of untrusted input gets a fuzz target with a seed corpus.
- **Shared contract tests:** a new adapter must pass its port's shared contract test suite.
- **Darwin binaries:** never strip or alter a built darwin binary.
- **Words:** use the [glossary](glossary.md)'s terms, one word for one meaning.
