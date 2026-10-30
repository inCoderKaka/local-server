# ADR 0001: req_001 scope and product decisions (discussion stage)

**Status:** Accepted
**Date:** 2026-10-30
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_001.md`
**Decisions:** D-01..D-19 in [`req_001.decisions.md`](../tracks/platform/phase_1/requirements/req_001.decisions.md). The log is the source; this ADR summarises it.
**executive_inputs_reference:** execetive_inputs/26-10-30_platform_req_001_discussion.md

## Context

local-server lets a host share their machine's AI inference with invited consumers and client apps. PA, the personal-assistant app, is the first client. The repo is greenfield: no code exists yet (F-03). The discussion stage settled scope, stack, exposure, login, engine install, usage integrity, the PA contract and the licence. The high-level and low-level design (the design-review stage, recorded in its own ADR) builds on these decisions. Terms are defined in [`docs/glossary.md`](../glossary.md).

## Decision

**Scope**
- D-01: The requirement is software-touching, so the system design review stages apply.
- D-02: Phase 1 builds only what invited consumers and PA need: the inference gateway, keys and grants, usage, resource requests and the Tailscale transport. Marketplace/discovery, an own relay server, billing/credits and non-inference resource kinds are seams (interfaces plus a data model that can hold them), not built.
- D-04: Phase 1 trims:
  - Notifications: in-app, webhooks and desktop only. Email and Telegram come later.
  - Install: curl/PowerShell scripts first. brew/winget and auto-update come later.
  - This repo never modifies PA's code.
  - Everything else is built in Phase 1: three operating systems, the background service, consumer accounts with invite sign-up and a web page, request types/tags, and ledger-grade usage.
- D-05..D-07: Added to Phase 1: a host status endpoint (`/v1/health`), a pause-sharing switch plus quiet hours, and an audit log of admin actions.
- D-08: Client-side failover across several hosts is scoped for the future. Identity and keys are designed so it fits later.

**PA contract**
- D-03: PA plugs in through a new adapter implementing its existing `LLMProvider` port. The adapter speaks local-server's OpenAI-compatible API with auth and quota/"request more" awareness, and falls back to PA's Gemini/Anthropic adapters when the host is offline or declines. PA's Ollama adapter is not extended.
- D-17: PA's requirement file is written after the HLD/LLD fix the API. It goes in `personal-assistant/docs/tracks/platform/phase_2/requirements/req_001.md` and is not committed.

**Stack and runtime**
- D-09: Go, as a single static binary per OS (macOS/Windows/Linux), with SQLite and the web UI built into the binary. Storage sits behind an interface so a hosted store can plug in later.
- D-10: The consumer API and consumer web page are served over the transport. The admin UI listens on localhost by default. An optional second listener on the host's private tailnet address sits behind an admin login. Nothing admin-related is ever public.
- D-11: The host queues work only while online, with interactive requests ahead of background ones. While the host is offline, the client app keeps its own retry queue for background work and sends interactive calls to its fallback.
- D-12: Tailscale is the Phase 1 default transport, behind a transport adapter. The public no-plaintext option was carried to the HLD as Q-16.
- D-14: The installer detects Ollama and asks before running Ollama's official installer. It never installs silently.
- D-15: Background-work time windows are one rule type in the request-properties policy, set per grant or globally. They are separate from quiet hours.

**Identity and integrity**
- D-13: Consumer login by password (hashed) and passkey (WebAuthn). Invite links carry a one-time sign-up token. Auth sits behind an adapter. Key-only consumers need no login.
- D-16: A tamper-evident, append-only usage ledger on the host, plus a host-signed usage receipt in every response. Consumers can fetch the host's public key.

**Licence**
- D-18: Apache-2.0, for permissive adoption with an explicit patent grant. PA only calls local-server over the network, so it is unaffected.

D-19 closed the discussion stage with 0 deferred items.

## Alternatives considered

The log records chosen options, not the rejected ones. Rejected alternatives: none recorded. Each row's "(answers Q-NN)" link names the question it settled.

## Consequences

- The data model and interfaces must hold future marketplace, relay, billing and multi-host data without being built now (D-02, D-08). This is extra design effort now in exchange for no identity or schema rework later.
- Three operating systems and a background service in Phase 1 widen the release and test matrix (D-04).
- The usage ledger and receipts need a host signing key pair from day one (D-16).
- PA gets resilience through its fallback adapters, so local-server need not queue interactive work while offline (D-03, D-11).
- Deferred trims (email, Telegram, brew/winget, auto-update) are tracked in the log, not lost (D-04).

## Sources

The discussion rows carry no external sources: none recorded. Repo facts used: F-01..F-05 in the decisions log. F-04 cites PA's `LLMProvider` port at `personal-assistant/apps/api/app/platform/llm/base.py:275` and `factory.py:1-4`.
