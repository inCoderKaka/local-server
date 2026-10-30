# platform — phase_1 design

The design summary for each requirement in this phase. The decisions log is the source of truth, and the ADRs summarise it. This file only points to them. Terms are defined in [`docs/glossary.md`](../../../glossary.md).

## req_001 — local-server: share a host's AI inference with invited consumers

Requirement: [`requirements/req_001.md`](requirements/req_001.md) · Decisions log: [`requirements/req_001.decisions.md`](requirements/req_001.decisions.md)

### discussion

Closed on 2026-10-30 (D-19), recorded in [ADR 0001](../../../adr/0001-req001-scope-and-product-decisions.md).
- **Scope (D-02, D-04..D-08):** Phase 1 builds the inference gateway, keys and grants, usage, resource requests and the Tailscale transport. It adds host status, pause sharing with quiet hours, and an audit log. Marketplace, relay, billing and multi-host failover are seams only. Email/Telegram notifications, brew/winget and auto-update come later.
- **PA (D-03, D-17):** PA connects through a new adapter on its `LLMProvider` port and falls back to its cloud providers. This repo only writes PA's requirement file and never changes PA's code.
- **Stack and exposure (D-09..D-12, D-14):**
  - One Go binary per OS, with SQLite and the web UI built in.
  - The consumer API is served over Tailscale. The admin UI is on localhost, with an optional tailnet listener behind a login.
  - The host queues work only while online.
  - The installer detects Ollama and asks before installing it.
- **Identity and integrity (D-13, D-15, D-16):** Consumers log in by password or passkey and sign up through invites. Key-only consumers are allowed. Background-work hours are a policy rule. There is a tamper-evident usage ledger and a signed receipt for each response.
- **Licence (D-18):** Apache-2.0.

### design-review

The HLD and LLD closed on 2026-10-30, recorded in [ADR 0002](../../../adr/0002-req001-system-design.md). The full design is in [`docs/architecture.md`](../../../architecture.md). AI architecture was skipped (D-20).
- **Shape (D-21, D-22):** a modular monolith with ports and adapters. The Ollama native API sits behind an `Engine` port, and local-server owns the model lifecycle and `num_ctx`.
- **Transport (D-23, D-24):** system Tailscale by default and embedded tsnet as an option. Funnel is an opt-in public mode with mandatory defence in depth.
- **Identity, scheduling, ledger (D-25..D-27, D-42):**
  - An Ed25519 host identity, consumers with keys and grants, a unified `idn_principal`, and a declarative policy.
  - Two-class scheduling with memory admission and async jobs.
  - A Merkle ledger with signed checkpoints and JWS receipts.
- **Storage and data (D-28, D-40, D-41, D-43, D-44):**
  - Pure-Go SQLite with a single writer and short module prefixes. Every table and column carries a classification and billable flag.
  - Content is never at rest except in encrypted job tables.
  - Off-host backups are decentralised by default, and platform sync is opt-in.
  - Per-dataset sharing grants; the Phase 1 grantee is the platform admin only.
- **Ops and contract (D-29..D-33):**
  - Notifications: in-app, webhooks via an outbox, and desktop.
  - A per-user service with curl/PowerShell installers and a sleep lock.
  - Telemetry and host-metrics ports.
  - A spec-first OpenAPI with standard surfaces unchanged and header extensions.
- **LLD (D-38..D-40):**
  - State machines with every value a runtime setting.
  - Call flows F1–F9, where metering failure still returns the answer and is alerted.
  - Data paths.
- **Future direction (D-45):** seams only. Legal traces live in [`docs/legal/`](../../../legal/README.md).
- **Before ticketize:** verify five unsourced claims: Funnel on the free plan, Gatekeeper with an unsigned binary, `OllamaSetup.exe` silent flags, Ollama truncation beyond `num_ctx`, and per-OS keychain support.
