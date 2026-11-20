# ADR 0007: req_002 system design: option resolution, discovery, the portal route and the low-level design

**Status:** Accepted
**Date:** 2026-11-21
**Extends:** [ADR 0006](0006-req002-discussion.md), [ADR 0002](0002-req001-system-design.md)
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_002.md`
**Decisions:** HLD D-20..D-25 and LLD D-26..D-30 in [`req_002.decisions.md`](../tracks/platform/phase_1/requirements/req_002.decisions.md). The log is the source.
**executive_inputs_reference:** docs/execetive_inputs/26-11-21_platform_req_002_design-review.md

## Context

[ADR 0006](0006-req002-discussion.md) fixed what req_002 adds: engine-neutral options, standard OpenAI fields, per-grant defaults, forced values and resource caps, capability discovery with a TypeScript library, a task tag, and the portal's use of this host. This ADR records how it is built. Everything here is designed now and built in phase 2, after `v0.1.0` (req_001 D-161). The AI-architecture rows (D-31 and later) are in a later ADR.

## Decision

**High-level design**
- **D-20:** effective options are resolved by extending the decision and enforcement split. Grants and policy rules own the defaults, forced values and resource caps, and `policy.Evaluate` merges them into its `Decision` (smallest cap wins). The gateway's planner combines the client's values, that decision and the model's capabilities from `ModelInfo`, refuses an option the model cannot carry, and writes the effective-options header. The engine adapter only maps the neutral names.
- **D-21:** task names are free-form and bounded: a short token, with a host-owned setting capping the distinct values per consumer per rollup window; values past the cap are recorded as `other`.
- **D-22:** the portal's Vercel deployment reaches the host from inside the function, with Tailscale's userspace-networking pattern for serverless functions, a tagged ephemeral auth key held as a Vercel secret, and a grant reaching only the host's consumer API port. There is no relay and no fallback: if it does not work on Vercel, the hosted portal has no `local` provider until public mode. This is the portal repo's build.
- **D-23:** discovery lives in both places. `GET /v1/capabilities` is the full per-caller answer, and `GET /v1/models` gains an OpenRouter-style `supported_parameters` name list per model. Both come from one source in the code, a test checks they agree, and a consistency checklist is written into the docs.
- **D-24:** thinking text is returned in a `reasoning` field on the message and on stream deltas. Thinking tokens count as output tokens against `max_tokens`, the quotas and the receipt; `reasoning_tokens` is reported only when the engine gives a count. Thinking text is content and is never logged or stored.
- **D-25:** the TypeScript client library lives in this repo (for example `clients/ts/`), its types are generated from `api/openapi.yaml` in the same commit as any spec change with a CI check, and it is published to npm with trusted publishing and provenance, versioned with semver against the API.

**Low-level design**
- **D-26 (state machines):** the chat stream gains a reasoning-chunk event with no new state, and the Ollama chunk reader surfaces thinking-only chunks so the gateway's first-token and idle limits see them. Jobs and model residency are unchanged, and a pinned model keeps keep-alive `-1`, so a client value never unpins it.
- **D-27 (call flows):** the chat call becomes: policy decision with the new caps and task, validation of the new fields, planning (forced value, then client value, then grant default; caps clamp and report; a client-sent option the model cannot carry is `unsupported_parameter`, a grant default or forced value it cannot carry is skipped and reported), admission with the effective keep-alive, the engine call with the new request fields, reasoning chunks, metering, and the effective-options header on streamed and non-streamed answers. Thinking is set by `reasoning_effort` alone. Jobs store the resolution with the sealed result. One builder serves `planChat`, `/v1/capabilities` and `/v1/models`. The Ollama mapping is `options.*`, `format` and `think`.
- **D-28 (data paths):** the task goes from the header through a character and length check to a new `use_record.task` column, and the rollup worker applies the per-day cap and writes `other`. It is not in the leaf or the receipt. Option values are not stored. Reasoning text is never stored. A `json_schema` goes only to the engine. Images go through a new `Message.Images` behind a `vision` check, which corrects F-06, and `/v1/usage` gains a task filter and field.
- **D-29 (schema):** grants migration 0004 adds `grt_version_model_option` (per grant version and model, `default` or `forced`, JSON value) and two cap columns on `grt_version_model` (`keep_alive_cap_seconds`, `thinking_allowed`). Usage migration 0005 adds `use_record.task` and rebuilds `use_rollup` with the task in its key. Policy rule `caps` gains optional `keep_alive_seconds` and `thinking` in format version 1; rules carry caps only. Four new host settings: `policy.task.max_length`, `usage.task.max_per_window`, `gateway.model_info_ttl` and `scheduler.keep_alive_max`.
- **D-30 (relationships):** the option table hangs off `grt_version_model` (cascade); the task label has no foreign key and the rollup keeps the sum-per-bucket invariant; an unknown stored option name is skipped with a log line.

## Alternatives considered

- **D-20:** resolving inside the engine adapter, which would leak grants into the anti-corruption layer; a separate options module, which would duplicate policy's merge rules.
- **D-21:** a host-registered task list per grant, and free-form unbounded values.
- **D-22:** a spike with a Fly.io relay fallback, and a relay from the start.
- **D-23:** `/v1/capabilities` only, and `/v1/models` only.
- **D-24:** not returning thinking text, and the `reasoning_content` name.
- **D-25:** a git dependency, and the library in the portal repo.
- **D-28:** capping tasks at write time, and a receipt-only task field.
- **D-29:** options per grant instead of per model, rules carrying defaults and forced values, and a CHECK list of option names.
- **D-30:** failing closed on an unknown stored option name.
- **D-26, D-27:** none recorded.

## Consequences

- One resolution point means the effective-options header cannot disagree with what ran, and a second engine adapter repeats no cap logic (D-20).
- The hosted portal's local provider depends on an unproven pattern; the user accepted the risk and chose no fallback, so a failure means no local provider on the hosted portal until public mode (D-22).
- Two migrations and a rebuild of `use_rollup` are needed, and old rows keep working: no option rows, no new caps, no task (D-29).
- The per-model keep-alive that is stored today but unused is wired in as the default (D-27).
- Image support needs wiring that F-06 assumed already existed (D-28).
- Rules can narrow caps but cannot set defaults or forced values, which narrows D-20's wording; a rule with the new caps is invalid for an older binary, acceptable because migrations only go forward (D-29).
- The library adds an npm release process to this repo, versioned against the API (D-25).
- The spec's field names and the header's names are settled when the spec ticket is written.

## Sources

- **D-22:** [Tailscale serverless integrations](https://tailscale.com/docs/integration-serverless-apps) (Vercel not listed), [Tailscale on AWS Lambda](https://tailscale.com/docs/install/cloud/aws/aws-lambda), [userspace networking](https://tailscale.com/docs/concepts/userspace-networking), [ephemeral nodes](https://tailscale.com/docs/features/ephemeral-nodes), [auth keys](https://tailscale.com/docs/features/access-control/auth-keys), [Tailscale pricing FAQ](https://tailscale.com/docs/reference/faq/pricing), [Vercel function limits](https://vercel.com/docs/functions/limitations), [Vercel runtimes](https://vercel.com/docs/functions/runtimes), [Vercel daemons](https://vercel.com/kb/guide/vercel-functions-containers-daemons), [Fly.io pricing](https://docs.fly.io/about/pricing). **Unsourced:** that `tailscaled` runs inside a Vercel function at all, its cold-start cost, and whether userspace mode resolves MagicDNS names; grants limiting a tag to one port were seen only in a search summary ([grants syntax](https://tailscale.com/docs/reference/syntax/grants)).
- **D-23:** [OpenRouter models API](https://openrouter.ai/docs/api-reference/models/get-models), [OpenRouter models guide](https://openrouter.ai/docs/guides/overview/models), [LiteLLM model discovery](https://docs.litellm.ai/docs/proxy/model_discovery), [llama.cpp server README](https://raw.githubusercontent.com/ggml-org/llama.cpp/HEAD/tools/server/README.md), [openai-python unknown response fields](https://raw.githubusercontent.com/openai/openai-python/main/src/openai/_models.py), [openai-node](https://raw.githubusercontent.com/openai/openai-node/master/README.md), [Ollama show-model details](https://docs.ollama.com/api-reference/show-model-details).
- **D-24:** [Ollama thinking](https://docs.ollama.com/capabilities/thinking), [Ollama OpenAI layer source](https://raw.githubusercontent.com/ollama/ollama/main/openai/openai.go), [Ollama OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility), [vLLM reasoning outputs](https://docs.vllm.ai/en/stable/features/reasoning_outputs/), [OpenRouter reasoning tokens](https://openrouter.ai/docs/use-cases/reasoning-tokens), [DeepSeek thinking mode](https://api-docs.deepseek.com/guides/thinking_mode), [OpenAI chat completions reference](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create), [OpenAI reasoning guide](https://developers.openai.com/api/docs/guides/reasoning), Ollama issues [19093](https://github.com/ollama/ollama/issues/19093) and [14957](https://github.com/ollama/ollama/issues/14957). **Unsourced:** an official Ollama statement that `eval_count` includes thinking tokens.
- **D-25:** [openai-python](https://github.com/openai/openai-python), [openai-node](https://raw.githubusercontent.com/openai/openai-node/master/README.md) and [issue 924](https://github.com/openai/openai-node/issues/924), [Vercel AI SDK compatible chat model](https://github.com/vercel/ai/blob/main/packages/openai-compatible/src/chat/openai-compatible-chat-language-model.ts), [vLLM `extra_body`](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/), [openapi-typescript](https://openapi-ts.dev/introduction), [npm trusted publishing](https://docs.npmjs.com/trusted-publishers), [npm git dependencies](https://docs.npmjs.com/cli/v11/configuring-npm/package-json), [Semantic Versioning](https://semver.org/). The evidence does not bear out HLD §6.2's "SDKs may drop unknown fields" (F-12). Noted for later: `openapi-fetch` is [entering maintenance mode](https://github.com/openapi-ts/openapi-typescript/discussions/2559).
- **D-26:** [Ollama scheduler source](https://raw.githubusercontent.com/ollama/ollama/main/server/sched.go), [Ollama FAQ](https://docs.ollama.com/faq), [Ollama issue 11002](https://github.com/ollama/ollama/issues/11002). **Unsourced:** what Ollama does when a second request shortens `keep_alive` while the first is still running.
- **D-27:** [Ollama OpenAPI spec](https://docs.ollama.com/openapi.yaml), [structured outputs](https://docs.ollama.com/capabilities/structured-outputs), [thinking](https://docs.ollama.com/capabilities/thinking), [OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility), [OpenAI layer source](https://raw.githubusercontent.com/ollama/ollama/main/openai/openai.go), [API types](https://raw.githubusercontent.com/ollama/ollama/main/api/types.go). **Unsourced:** whether `eval_count` includes thinking tokens, a live `/api/show` response listing `tools`, and Ollama's behaviour for `think: false` on a model without thinking.
- **D-28:** [Ollama chat API](https://docs.ollama.com/api/chat) and [vision](https://docs.ollama.com/capabilities/vision). **Unsourced:** that the native chat endpoint needs no `data:` prefix on `images`.
- **D-20, D-21, D-29, D-30:** none recorded (repo references only).
