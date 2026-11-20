# ADR 0006: req_002 discussion: portal integration and more tunable request parameters

**Status:** Accepted
**Date:** 2026-11-21
**Extends:** [ADR 0002](0002-req001-system-design.md)
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_002.md`
**Decisions:** D-01..D-19 in [`req_002.decisions.md`](../tracks/platform/phase_1/requirements/req_002.decisions.md). The log is the source.
**executive_inputs_reference:** docs/execetive_inputs/26-11-21_platform_req_002_discussion.md

## Context

The owner asked whether it is too late to add more tunable parameters to the client request, along with personal-assistant (PA), and wants a good integration with the interactive-learning-portal (the portal), using this machine as a host for some of the portal's AI actions. The portal has many tunable parameters per task, and the integration has to be standard and consistent. The consumer API today accepts `temperature`, `top_p`, `seed`, `stop` and the output length, and refuses `frequency_penalty`, `presence_penalty`, `tools` and a `response_format` other than `text` (F-02). Request fields are validated one by one and unknown ones are refused, so new fields are additive (D-04). The design is done now and built in phase 2, after `v0.1.0`, and the phase 1 scope freeze stands (D-02, req_001 D-161).

## Decision

- **D-01:** the requirement is software-touching, so the system design review and AI-architecture stages run.
- **D-02:** carried from req_001, not re-opened: engine options the OpenAI API lacks travel in one namespaced object in the chat request body, validated field by field; the portal reaches local-server through a server-side route of its own, and no listener answers CORS (req_001 D-148, D-161..D-163).
- **D-03, D-11:** the engine-options object uses engine-neutral names owned by local-server, and each engine adapter maps them. The fixed list is `top_k`, `min_p`, `repeat_penalty`, `context_size` and `keep_alive`; a later option is a deliberate spec, adapter and test change.
- **D-04:** what already serves part of the requirement: per-field validation, the Ollama adapter's single options struct, the model's context length and capability list from the engine port, grant caps on context and output length, `LS-Request-Properties` (read only by policy), and image inputs.
- **D-05:** the change set to design: capability discovery, JSON and JSON-schema output, thinking with its tokens metered, per-option grant caps, usage by task, and the portal's own server route.
- **D-06:** every answer reports, in a response header, the engine options and sampling values that actually ran after caps, defaults and host overrides, so nothing is clamped silently.
- **D-07:** the host can set, per grant, a default for an option (a client value replaces it within the caps) or a forced value that replaces the client's value; either way the answer reports what ran.
- **D-08:** model classes `fast`, `mid` and `strong` labelled by the host, returned by discovery, are scoped for the future.
- **D-09:** a client may set keep-alive within a host cap; the host's keep-alive stays the default.
- **D-10:** the portal's Vercel deployment joins the tailnet to reach the host through its server-side route; other hosted apps wait for public mode.
- **D-12:** `frequency_penalty`, `presence_penalty`, `response_format` (`text`, `json_object`, `json_schema`) and `reasoning_effort` become standard body fields, all optional; a model that cannot carry one answers `unsupported_parameter` naming it.
- **D-13:** "standard and consistent" means capability discovery plus a small TypeScript client library, so clients build their settings from the host's answer and not from a hard-coded table.
- **D-14:** range caps per grant apply only to resource options (`context_size`, output length, `keep_alive`, thinking allowed); every other option can only get a default or a forced value.
- **D-15:** a client names the task behind a call with one standard key in `LS-Request-Properties`; usage records it as a new dimension and policy rules can match on it.
- **D-16:** the portal may run its text and vision tasks on this host; realtime voice stays on cloud providers, with a seam kept for a future audio API.
- **D-17, D-18:** the portal's code changes only from the portal repo, so this repo keeps a requirement file there (never committed from here), and PA moves to the same contract in the same release.
- **D-19:** the discussion stage closed on 2026-11-21.

## Alternatives considered

- **D-03:** Ollama's option names verbatim, rejected because it ties the contract to one engine.
- **D-05:** dropping usage by task, or per-option caps; the user chose to design all of them.
- **D-10:** only a self-run portal on a tailnet machine, or public mode for the portal too.
- **D-11:** only the options the portal uses, or every engine option (mirostat and the like), rejected because they are hard to cap and tie the contract to one engine in practice.
- **D-12:** only `response_format`, or putting these fields in the namespaced object too.
- **D-13:** discovery alone, or the developer guide alone.
- **D-14:** ranges for every option, or no new caps.
- **D-15:** a separate `LS-Task` header.
- **D-16:** text tasks only, or a fixed subset of tasks.
- **D-17:** this repo changing the portal's code, or nothing written into the portal repo.
- **D-18:** PA adopting the contract later.
- **D-02, D-04, D-06..D-09, D-19:** none recorded.

## Consequences

- Old clients keep working, because every new field is optional and unknown fields were already refused (D-04, D-12).
- The contract grows by a fixed, spec-owned option list, so each later option is a spec, adapter and test change (D-03, D-11).
- Grant versions, the admin and consumer pages and usage rollups gain new data (per-option settings, resource caps, a task dimension), each with a migration to design in the later stages (D-05, D-07, D-14, D-15).
- Whether a Vercel function can join a tailnet, and at what cold-start and secret-handling cost, was not verified when this stage closed; if it fails, the hosted portal has no local provider until public mode (D-10).
- Keep-alive set by clients trades faster follow-up calls for memory held longer on a shared host (D-09).
- Forced values and defaults mean a client's request can run with values it did not send, which the answer's header reports (D-06, D-07).
- The portal and PA requirement files are written from this repo and never committed from here, so both clients stay consistent by being rewritten when the API changes (D-17, D-18).
- Further stages: the high-level design, low-level design and AI-architecture decisions of this requirement are summarised in later ADRs (D-20 and after).

## Sources

None recorded: the discussion-stage rows carry repo references only (`depends-on` in the log). The research-backed rows are the HLD and later ones.
