# Platform track — next steps (ordered)

Convention: `docs/tracks/README.md`. Hand-over: `/orchestrate-implement platform 1`.

## req_001 — local-server phase 1 (ticketized 2026-10-30)

One item per step, in batch › slice order; `∥` = slices that may run in parallel (max 3 builders). **gate it** = touches a shared contract or a security surface, so run the review gate on it. A slice whose ticket edits `api/openapi.yaml` serialises with any other spec edit.


### B1 — Foundations

1. ~~S1 **PLT-C-001** Go module scaffold, CI on 3 OSes and module-boundary check (deps: —) **gate it**~~ done 2026-10-31 (`37c2603`, `d588ee1`)
2. ~~S2 **PLT-F-001** OpenAPI 3.1 contract v1 (consumer, consumer self-service and admin) with generated server code (deps: PLT-C-001) **gate it**  ∥  S3 **PLT-F-002** Storage foundation: single writer, cross-module unit of work, migrations, change capture, data catalog (deps: PLT-C-001) **gate it**  ∥  S4 **PLT-F-003** Telemetry port: slog JSON, redaction, Prometheus metrics, request id, log rotation (deps: PLT-C-001)~~ built 2026-10-31, not shipped (`f00fa8a`, `6839035`, `ef86df3`; set B code gate ran, `2da4671`)
3. ~~S5 **PLT-F-004** Runtime settings with owners, plus retention registry (deps: PLT-F-002) **gate it**~~ built 2026-10-31, not shipped (`5cd11e2`; set C code gate ran, `fab7890`)
4. ~~S6 **PLT-F-005** Host identity, signing subkeys and sealed secrets (deps: PLT-F-002, PLT-F-004) **gate it**~~ built 2026-10-31, not shipped (`939208f`, `fab7890`; set C code gate ran)

### B2 — Core domain

5. ~~S7 **PLT-F-006** Engine port with Ollama adapter, fake engine and adapter contract suite (deps: PLT-C-001)  ∥  S8 **PLT-F-007** Identity and grants data model (deps: PLT-F-002) **gate it**~~ built 2026-10-31, not shipped (`5cd11e2`; set C code gate ran, `fab7890`)
6. ~~S9 **PLT-F-008** Audit log (deps: PLT-F-007)  ∥  S10 **PLT-F-009** Domain events core (deps: PLT-F-002, PLT-F-007) **gate it**~~ built 2026-10-31, not shipped (`939208f`, `fab7890`; set C code gate ran)
7. ~~S11 **PLT-F-010** Access keys: issue, authenticate, expire, revoke (deps: PLT-F-007, PLT-F-008, PLT-F-009) **gate it**  ∥  S12 **PLT-F-011** Policy engine for request properties, background hours and priority class (deps: PLT-F-007) **gate it**  ∥  S13 **PLT-F-012** Host sharing state: pause switch and quiet hours (deps: PLT-F-004, PLT-F-005, PLT-F-008, PLT-F-009)~~ built 2026-10-31, not shipped (`8d9cf62`; set D code gate owed)
8. ~~S14 **PLT-F-013** Host metrics: totals, share-budget slice and other apps (deps: PLT-F-002, PLT-F-004, PLT-F-003, PLT-F-006)~~ built 2026-10-31, not shipped (`9a2d64a`, `605c398`; set D code gate ran, `ac8a183`)

### B3 — Inference path

9. ~~S15 **PLT-F-014** Scheduler, share budget and model lifecycle (deps: PLT-F-004, PLT-F-006, PLT-F-011, PLT-F-012)  ∥  S16 **PLT-F-015** Usage records, Merkle ledger leaves and signed receipts (deps: PLT-F-001, PLT-F-002, PLT-F-005) **gate it**~~ built 2026-10-31, not shipped (`e1089c9`, `5cf1311`; set E code gate ran, `551f356`)
10. ~~S17 **PLT-F-016** Ledger checkpoints, proofs and key publication (deps: PLT-F-015) **gate it**  ∥  S18 **PLT-F-017** Quotas, rate limits, request-size limits and key lockout (deps: PLT-F-004, PLT-F-007, PLT-F-010, PLT-F-015, PLT-F-009) **gate it**~~ built 2026-10-31, not shipped (`e8378b4`, `957b5af`, `46e5d77`; set E code gate ran, `551f356`)
11. S19 **PLT-F-018** Gateway: OpenAI-compatible chat and embeddings (non-streaming) (deps: PLT-F-001, PLT-F-003, PLT-F-006, PLT-F-010, PLT-F-011, PLT-F-014, PLT-F-015, PLT-F-017) **gate it**
12. S20 **PLT-F-019** Gateway streaming, timeouts and disconnect handling (deps: PLT-F-018)  ∥  S21 **PLT-F-020** Startup, first-run setup, module registry and doctor command (deps: PLT-F-004, PLT-F-003, PLT-F-005, PLT-F-006, PLT-F-018)  ∥  S22 **PLT-F-021** Host status, capabilities and models endpoints (deps: PLT-F-018, PLT-F-017)
13. S23 **PLT-F-022** Async jobs API for background work (deps: PLT-F-018, PLT-F-009)  ∥  S24 **PLT-F-023** Usage rollups and usage endpoint (deps: PLT-F-004, PLT-F-010, PLT-F-015)

### B4 — Control plane and accounts

14. S25 **PLT-F-024** Admin API base: admin listener, login, first admin, settings and host controls (deps: PLT-F-001, PLT-F-004, PLT-F-003, PLT-F-008, PLT-F-012, PLT-F-013, PLT-F-020) **gate it**
15. S26 **PLT-F-025** Admin API: consumers, invites and access keys (deps: PLT-F-024, PLT-F-007, PLT-F-010, PLT-F-015)  ∥  S27 **PLT-F-026** Admin API: grants, models and policy rules (deps: PLT-F-024, PLT-F-006, PLT-F-007, PLT-F-011)  ∥  S28 **PLT-C-002** Terms of Use and Privacy/consent drafts v1 for the user's review (deps: —)
16. S29 **PLT-F-027** Consumer accounts: invites, password and passkey login, sessions, consent (deps: PLT-F-024, PLT-F-025, PLT-C-002) **gate it**
17. S30 **PLT-F-028** Resource requests with comments (deps: PLT-F-026, PLT-F-010, PLT-F-027, PLT-F-009)

### B5 — Notifications

18. S31 **PLT-F-029** Notifications: preferences, in-app store and live updates (deps: PLT-F-009, PLT-F-024, PLT-F-027, PLT-F-028)
19. S32 **PLT-F-030** Webhooks: Standard Webhooks delivery through an outbox (deps: PLT-F-029, PLT-F-005) **gate it**  ∥  S33 **PLT-F-031** Desktop notifications for the host (deps: PLT-F-029)

### B6 — Web UIs

20. S34 **PLT-F-032** Admin UI shell and dashboard (deps: PLT-F-024, PLT-F-013, PLT-F-023, PLT-F-029) **gate it**
21. S35 **PLT-F-033** Admin UI: consumers, invites and access keys (deps: PLT-F-032, PLT-F-025)  ∥  S36 **PLT-F-034** Admin UI: grants, models and policy rules (deps: PLT-F-032, PLT-F-026)  ∥  S37 **PLT-F-035** Admin UI: requests, usage, audit, notifications and settings (deps: PLT-F-032, PLT-F-028, PLT-F-030, PLT-F-023, PLT-F-008)
22. S38 **PLT-F-036** Consumer web page: shell, invite sign-up and login (deps: PLT-F-032, PLT-F-027)
23. S39 **PLT-F-037** Consumer web page: limits, usage, keys, requests and notifications (deps: PLT-F-036, PLT-F-028, PLT-F-030, PLT-F-023)

### B7 — Transport

24. S40 **PLT-F-038** System Tailscale transport and listener layout (deps: PLT-F-018, PLT-F-020, PLT-F-024, PLT-F-036) **gate it**
25. S41 **PLT-F-039** Public mode through Tailscale Funnel with mandatory protections (deps: PLT-F-038, PLT-F-017, PLT-F-027, PLT-F-035) **gate it**  ∥  S42 **PLT-F-040** Embedded Tailscale (tsnet) optional build (deps: PLT-F-038)

### B8 — Packaging

26. S43 **PLT-F-041** Background service: per-user by default, system mode optional (deps: PLT-F-020)  ∥  S44 **PLT-F-042** Sleep lock while sharing (deps: PLT-F-012, PLT-F-020)  ∥  S45 **PLT-C-003** Release pipeline with checksums (deps: PLT-C-001, PLT-F-032, PLT-F-036) **gate it**
27. S46 **PLT-F-043** Install, upgrade and uninstall scripts (deps: PLT-F-041, PLT-C-003) **gate it**

### B9 — Off-host data and sharing

28. S47 **PLT-F-044** Backups to a local folder and the host's own bucket, and restore (deps: PLT-F-004, PLT-F-005, PLT-F-020)  ∥  S48 **PLT-F-045** Export and import of signed data bundles (deps: PLT-F-002, PLT-F-005) **gate it**  ∥  S49 **PLT-F-046** Opt-in platform sync with per-dataset keys (deps: PLT-F-002, PLT-F-004, PLT-F-005, PLT-F-020) **gate it**
29. S50 **PLT-F-047** Sharing grants with the platform admin (backend and admin tool) (deps: PLT-F-046, PLT-F-029, PLT-F-007) **gate it**
30. S51 **PLT-F-048** Admin UI: data-sharing approvals (deps: PLT-F-047, PLT-F-035)

### B10 — Docs

31. S52 **PLT-C-004** Host setup guide and README (deps: PLT-F-043, PLT-F-039)

Early starters (narrow deps, can fill a free builder slot ahead of their position): PLT-F-045. PLT-F-006 done 2026-10-31 (`5cd11e2`). PLT-C-002 done 2026-10-31 (`3cf4f3f`).

Before the consumer-accounts ticket (PLT-F-027) builds: the user approves the legal drafts (PLT-C-002, built; open points in each draft's review note).
