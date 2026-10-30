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
