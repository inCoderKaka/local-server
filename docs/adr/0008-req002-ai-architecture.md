# ADR 0008: req_002 AI architecture: traceability, trust boundary, the pipeline graph and thinking tokens

**Status:** Accepted
**Date:** 2026-11-21
**Extends:** [ADR 0007](0007-req002-system-design.md), [ADR 0006](0006-req002-discussion.md)
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_002.md`
**Decisions:** AI rows D-31..D-38 in [`req_002.decisions.md`](../tracks/platform/phase_1/requirements/req_002.decisions.md). The log is the source.
**executive_inputs_reference:** docs/execetive_inputs/26-11-21_platform_req_002_ai-architecture.md

## Context

req_002 puts AI calls from two client apps (the interactive-learning-portal and personal-assistant) through this host's models. The host adds no prompt, agent or graph of its own, so the AI-architecture stage covers the one model call it serves, how a client traces and saves what happened, and how far the host trusts and shapes model output. The three traceability rules (a trace per run, atomic and tunable steps, everything saved) were audited for the host's call and for both clients; every gap is closed by a decision below or deferred. Built in phase 2, after `v0.1.0`.

## Decision

- **D-31:** the requirement has an AI component: the portal's and personal-assistant's calls on this host's models. The host has no prompt, agent or graph of its own.
- **D-32 (trace):** the gateway resolves the model's digest from the engine's model list (cached with the model-info cache) and returns it as `system_fingerprint` in the non-streamed answer and in every stream chunk. It also stores the full digest in a new nullable `use_record.model_digest` column, added to usage migration 0005 and kept out of the canonical leaf and the receipt. The run id is the completion id and the signed receipt. The digest can lag a model pull by up to the model-info time-to-live. The client's own trace should use OpenTelemetry GenAI attribute names.
- **D-33 (atomic steps):** the host's chat call is one atomic step with client-tunable, echoed parameters. Client apps own tunable and comparable steps: the portal's and personal-assistant's requirement files must require per-step parameters and prompt version, change and re-run, and side-by-side run comparison built from the host's stable fields. The host builds no compare or replay tool.
- **D-34 (saved I/O):** client apps persist every AI request, response and the user's feedback, not sampled, with the effective options, prompt version, model and fingerprint, task, usage id, latency, finish reason, token counts, regeneration link and error. The host stays content-free by default except the existing sealed job content. Optional persistence by the host on the client's explicit consent is accepted in principle and is off by default; its mechanism is deferred (Q-25, blocking only a phase-2 slice).
- **D-35 (model):** pass-through. The host serves exactly the requested model and options or returns a typed error. It substitutes no model, has no fallback, and validates or repairs no JSON; the client validates its schema and owns fallback.
- **D-36 (prompt):** the host adds, rewrites or injects no prompt text. The prompt version is the client's trace field. The effective-options header also states the overflow policy in force (`refuse` or `truncate`).
- **D-37 (graph):** the chat pipeline becomes a named-node graph in this repo (no external framework): nodes `identify`, `evaluate`, `validate`, `plan`, `admit`, `infer` (the only model call) and `meter`, over one typed `CallState`. A content-free step trace is always on, stored in a new nullable `use_record.steps` JSON column in migration 0005 (outside the leaf and receipt). Checkpoints that hold content, and replay from `plan` or `infer`, exist only for calls the client consented to persist (Q-25) and for a job while its sealed payload exists. No interrupts and no retries of the model call.
- **D-38 (tokens):** the output cap follows OpenAI semantics: it covers thinking, the host adds no headroom, `finish_reason: length` is reported honestly, and the TypeScript library raises a typed thinking-exhausted signal. A grant default `reasoning_effort` can protect small caps, a contract test pins that Ollama counts thinking tokens in `num_predict`, and a numeric thinking budget is noted as a future engine option.

## Alternatives considered

- **D-33:** a replay or compare tool in the host's admin UI.
- **D-34:** an opt-in content log on the host as the default design, and a waiver.
- **D-35:** host-side retry on invalid JSON, and a host-side fallback model.
- **D-36:** also sending a `prompt_version` property to the host.
- **D-37:** the fixed pipeline with no graph (the recommendation, not taken), an external Go framework, and a step trace in metrics only.
- **D-38:** the host adding headroom when thinking is on.
- **D-31, D-32:** none recorded.

## Consequences

- A client can name the exact weights that served a call, and the host owner can see which digest served each record after a model update. The digest is as fresh as the model-info cache.
- The host stays private by default: content is stored only by client apps, or on a client's explicit consent once Q-25 is designed.
- The portal's and personal-assistant's requirement files carry the trace, comparison and saved-fields rules, so those are built in the client repos.
- Refactoring the chat pipeline into a node graph is a build cost (estimated M) that the fixed pipeline would have avoided; the user chose it for visible, tunable steps, a per-step trace and consented replay.
- `use_record` gains two nullable columns in migration 0005 (`model_digest`, `steps`) beyond ADR 0007's list. Neither changes a leaf hash.
- Replay is a new run with its own usage id and is not the same answer unless a seed is set.
- An empty answer is possible when thinking uses the whole cap, so clients must allow headroom; the library signals it.
- Q-25 stays deferred: the host-side persistence mechanism, the checkpoint store and the replay API are designed in phase 2.

## Sources

- **D-32:** [Ollama `/api/tags`](https://docs.ollama.com/api/tags), [Ollama OpenAPI schema](https://docs.ollama.com/openapi.yaml), [Ollama OpenAI layer](https://raw.githubusercontent.com/ollama/ollama/main/openai/openai.go), [OpenAI cookbook, `system_fingerprint`](https://developers.openai.com/cookbook/examples/reproducible_outputs_with_the_seed_parameter) (an older page), [vLLM fingerprint modes](https://docs.vllm.ai/en/stable/api/vllm/entrypoints/serve/utils/fingerprint/), [OpenTelemetry GenAI inference attributes](https://raw.githubusercontent.com/open-telemetry/semantic-conventions-genai/main/docs/gen-ai/client-inference.md) (Development stability; content is opt-in). **Unsourced:** that `/api/tags` and `/api/ps` return the same digest for one model on a live server.
- **D-33:** [LangSmith, compare experiments](https://docs.langchain.com/langsmith/compare-experiment-results.md), [Langfuse, compare experiments](https://langfuse.com/docs/evaluation/experiments/compare-experiments).
- **D-35:** [Ollama structured outputs](https://docs.ollama.com/capabilities/structured-outputs), [Ollama thinking](https://docs.ollama.com/capabilities/thinking), [Ollama issue 14645](https://github.com/ollama/ollama/issues/14645) (an issue thread: `format` ignored with `think: false` on one model, fixed later per a comment).
- **D-38:** [Ollama thinking](https://docs.ollama.com/capabilities/thinking), [Ollama issue 17561](https://github.com/ollama/ollama/issues/17561) (a thinking-budget proposal, open since 2026-08-04), [Ollama issue 14793](https://github.com/ollama/ollama/issues/14793) and [hermes-agent issue 46131](https://github.com/NousResearch/hermes-agent/issues/46131) (issue threads, 2026), [OpenAI reasoning guide](https://developers.openai.com/api/docs/guides/reasoning), [OpenRouter reasoning tokens](https://openrouter.ai/docs/use-cases/reasoning-tokens), [Anthropic extended thinking](https://platform.claude.com/docs/en/build-with-claude/extended-thinking), [Gemini thinking](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/thinking), [vLLM reasoning outputs](https://docs.vllm.ai/en/stable/features/reasoning_outputs/). **Unsourced:** an official Ollama statement that `num_predict` includes thinking tokens or that exhaustion gives `done_reason: length`; whether OpenRouter or LiteLLM add headroom for Ollama-style backends.
- **D-31, D-34, D-36, D-37:** none recorded (repo references and the user's choices only).
