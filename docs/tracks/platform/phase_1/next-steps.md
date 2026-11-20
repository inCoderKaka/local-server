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
7. ~~S11 **PLT-F-010** Access keys: issue, authenticate, expire, revoke (deps: PLT-F-007, PLT-F-008, PLT-F-009) **gate it**  ∥  S12 **PLT-F-011** Policy engine for request properties, background hours and priority class (deps: PLT-F-007) **gate it**  ∥  S13 **PLT-F-012** Host sharing state: pause switch and quiet hours (deps: PLT-F-004, PLT-F-005, PLT-F-008, PLT-F-009)~~ built 2026-10-31, not shipped (`8d9cf62`; set D code gate ran, `0640bf2`, `ac8a183`)
8. ~~S14 **PLT-F-013** Host metrics: totals, share-budget slice and other apps (deps: PLT-F-002, PLT-F-004, PLT-F-003, PLT-F-006)~~ built 2026-10-31, not shipped (`9a2d64a`, `605c398`; set D code gate ran, `ac8a183`)

### B3 — Inference path

9. ~~S15 **PLT-F-014** Scheduler, share budget and model lifecycle (deps: PLT-F-004, PLT-F-006, PLT-F-011, PLT-F-012)  ∥  S16 **PLT-F-015** Usage records, Merkle ledger leaves and signed receipts (deps: PLT-F-001, PLT-F-002, PLT-F-005) **gate it**~~ built 2026-10-31, not shipped (`e1089c9`, `5cf1311`; set E code gate ran, `551f356`)
10. ~~S17 **PLT-F-016** Ledger checkpoints, proofs and key publication (deps: PLT-F-015) **gate it**  ∥  S18 **PLT-F-017** Quotas, rate limits, request-size limits and key lockout (deps: PLT-F-004, PLT-F-007, PLT-F-010, PLT-F-015, PLT-F-009) **gate it**~~ built 2026-10-31, not shipped (`e8378b4`, `957b5af`, `46e5d77`; set E code gate ran, `551f356`)
11. ~~S19 **PLT-F-018** Gateway: OpenAI-compatible chat and embeddings (non-streaming) (deps: PLT-F-001, PLT-F-003, PLT-F-006, PLT-F-010, PLT-F-011, PLT-F-014, PLT-F-015, PLT-F-017) **gate it**~~ built 2026-11-01, not shipped (ab12bd8; set F gate ba604f9, fabd1d0)
12. ~~S20 **PLT-F-019** Gateway streaming, timeouts and disconnect handling (deps: PLT-F-018)~~ built 2026-11-01 (289168a; set G gate done: 2a95927, 97e96f5)  ∥  ~~S21 **PLT-F-020** Startup, first-run setup, module registry and doctor command (deps: PLT-F-004, PLT-F-003, PLT-F-005, PLT-F-006, PLT-F-016, PLT-F-018)~~ built 2026-11-02, not shipped (`b4fe621`; set A code gate ran, `229776d`; folds PLT-B-014, fixes PLT-B-039, PLT-B-012, PLT-B-015)  ∥  ~~S22 **PLT-F-021** Host status, capabilities and models endpoints (deps: PLT-F-018, PLT-F-017)~~ built 2026-11-01 (c767daa; set G gated)
13. ~~S24 **PLT-F-023** Usage rollups and usage endpoint (deps: PLT-F-004, PLT-F-010, PLT-F-015)~~ (831b93f)

### B4 — Control plane and accounts

14. ~~S25 **PLT-F-024** Admin API base: admin listener, login, first admin, settings and host controls (deps: PLT-F-001, PLT-F-004, PLT-F-003, PLT-F-008, PLT-F-012, PLT-F-013, PLT-F-020) **gate it**~~ (455b281)
15. ~~PLT-F-071 quota API shape~~ built 2026-11-05, provisional (`d03d916`, I-313; gated `734ae20`) — ~~PLT-F-096 windowed quota list, billing_mode, persisted counter~~ built 2026-11-06 (`a0124e8`, `400ee0b`; code gate ran 2026-11-07, `05bf043`) — S26 ~~**PLT-F-025** Admin API: consumers, invites and access keys~~ built 2026-11-06 (`4b94c59`; gated `734ae20`) (deps: PLT-F-024, PLT-F-007, PLT-F-010, PLT-F-015)  ∥  S27 ~~**PLT-F-026** Admin API: grants, models and policy rules~~ built 2026-11-06 (`79d0973`; code gate ran 2026-11-07 with PLT-F-096, `05bf043`) (deps: PLT-F-024, PLT-F-006, PLT-F-007, PLT-F-011, PLT-F-096)  ∥  ~~S28 **PLT-C-002** Terms of Use and Privacy/consent drafts v1 for the user's review (deps: —)~~ done (drafts await the user's approval)
16. S29 **PLT-F-027** Consumer accounts: invites, password and passkey login, sessions, consent (deps: PLT-F-024, PLT-F-025, PLT-C-002) **gate it**
17. S30 **PLT-F-028** Resource requests with comments (deps: PLT-F-026, PLT-F-010, PLT-F-027, PLT-F-009)

### B5 — Notifications

18. S31 **PLT-F-029** Notifications: preferences, in-app store and live updates (deps: PLT-F-009, PLT-F-024, PLT-F-027, PLT-F-028)
19. S32 **PLT-F-030** Webhooks: Standard Webhooks delivery through an outbox (deps: PLT-F-029, PLT-F-005) **gate it**  ∥  S33 **PLT-F-031** Desktop notifications for the host (deps: PLT-F-029)

### B6 — Web UIs

20. S34 **PLT-F-032** Admin UI shell and dashboard (deps: PLT-F-024, PLT-F-013, PLT-F-023, PLT-F-029) **gate it**
21. S35 **PLT-F-033** Admin UI: consumers, invites and access keys (deps: PLT-F-032, PLT-F-025)  ∥  S36 **PLT-F-034** Admin UI: grants, models and policy rules (deps: PLT-F-032, PLT-F-026)  ∥  S37 **PLT-F-035** Admin UI: requests, usage, audit, notifications and settings (deps: PLT-F-032, PLT-F-028, PLT-F-030, PLT-F-023, PLT-F-008)
22. S38 ~~**PLT-F-036** Consumer web page: shell, invite sign-up and login~~ built 2026-11-15
23. S39 ~~**PLT-F-037** Consumer web page: limits, usage, keys, requests and notifications~~ built 2026-11-15

### B7 — Transport

24. S40 **PLT-F-038** System Tailscale transport and listener layout (deps: PLT-F-018, PLT-F-020, PLT-F-024, PLT-F-036) **gate it**
25. S41 **PLT-F-039** Public mode through Tailscale Funnel with mandatory protections (deps: PLT-F-038, PLT-F-017, PLT-F-027, PLT-F-035) **gate it**  ∥  S42 **PLT-F-040** Embedded Tailscale (tsnet) optional build (deps: PLT-F-038)

### B8 — Packaging

26. S43 ~~**PLT-F-041** Background service: per-user by default, system mode optional~~ built 2026-11-08 (`a193a07`, `17d39f2`; gated, set A, `8fea837`)  ∥  S44 ~~**PLT-F-042** Sleep lock while sharing~~ built 2026-11-06 (`a457ece`; gated `734ae20`)  ∥  S45 **PLT-C-003** Release pipeline with checksums (deps: PLT-C-001, PLT-F-032, PLT-F-036) **gate it**
27. S46 ~~**PLT-F-043** Install, upgrade and uninstall scripts~~ built 2026-11-15 (`9211b8d`; gate owed at end of M4)

### B9 — Off-host data and sharing

28. S47 ~~**PLT-F-044** Backups to a local folder and the host's own bucket, and restore~~ built 2026-11-08 (`7678f06`, `71862a6`; gated, set A, `8fea837`)  ∥  S48 **PLT-F-045** Export and import of signed data bundles (deps: PLT-F-002, PLT-F-005) **gate it**  ∥  S49 **PLT-F-046** Opt-in platform sync with per-dataset keys (deps: PLT-F-002, PLT-F-004, PLT-F-005, PLT-F-020, PLT-F-029) **gate it**
29. S50 **PLT-F-047** Sharing grants with the platform admin (backend and admin tool) (deps: PLT-F-046, PLT-F-029, PLT-F-007) **gate it**
30. S51 **PLT-F-048** Admin UI: data-sharing approvals (deps: PLT-F-047, PLT-F-035)

### B10 — Docs

31. S52 **PLT-C-004** Host setup guide and README (deps: PLT-F-043, PLT-F-039)

Early starters (narrow deps, can fill a free builder slot ahead of their position): PLT-F-045. PLT-F-006 done 2026-10-31 (`5cd11e2`). PLT-C-002 done 2026-10-31 (`3cf4f3f`).

Before the consumer-accounts ticket (PLT-F-027) builds: the user approves the legal drafts (PLT-C-002, built; open points in each draft's review note).

## req_002 — client options, discovery and the portal integration (ticketized 2026-11-21)

**Build phase 2** (req_001 D-164): these tickets are outside the phase 1 finish line, the milestones and the budget of `finish-plan.md`. One item per step, in batch › slice order; `∥` = slices that may run in parallel (max 3 builders). **gate it** = touches a shared contract or a security surface, reviewed at the milestone gate (req_001 D-143). Every slice whose ticket edits `api/openapi.yaml` serialises with any other spec edit; PLT-F-114 holds the whole spec change first, so later spec edits are small.

### B11 — Contract and engine

1. S53 **PLT-F-114** Spec for the whole req_002 contract (deps: —) **gate it**
2. S54 **PLT-F-115** Option table, engine port and Ollama mapping (deps: PLT-F-114) **gate it**  ∥  S55 **PLT-F-116** Grants: per-model options and resource caps storage (deps: PLT-F-114) **gate it**
3. S56 **PLT-F-117** Grants admin API: per-model options and caps (deps: PLT-F-115, PLT-F-116)  ∥  S57 **PLT-C-021** Real-Ollama contract tests, first set (deps: PLT-F-115)

### B12 — Policy, planner and header

4. S58 **PLT-F-118** Policy decision: defaults, forced values, caps, task and rule caps (deps: PLT-F-116) **gate it**  ∥  S59 **PLT-F-119** Reasoning output (deps: PLT-F-115)  ∥  S60 **PLT-F-120** Model-info cache and support-list builder (deps: PLT-F-115)
5. S61 **PLT-F-121** Planner: option resolution and capability check (deps: PLT-F-118, PLT-F-120) **gate it**
6. S62 **PLT-F-122** Effective-options header (deps: PLT-F-121) **gate it**
7. S63 **PLT-F-123** Client keep-alive within a cap (deps: PLT-F-122)
8. S64 **PLT-F-124** Jobs carry options, task and the header (deps: PLT-F-123, PLT-F-119) **gate it**

### B13 — Discovery, images and usage by task

9. S65 **PLT-F-125** Capability discovery endpoints (deps: PLT-F-117, PLT-F-120, PLT-F-121)  ∥  S66 **PLT-F-126** Image input for vision (deps: PLT-F-121, PLT-F-124) **gate it**  ∥  S67 **PLT-F-127** Usage storage: migration 0005 and record columns (deps: PLT-F-118) **gate it**
10. S68 **PLT-F-128** Usage by task: grouping, cap and invariant (deps: PLT-F-127)
11. S69 **PLT-F-129** Usage API: task filter and field (deps: PLT-F-128)
12. S70 **PLT-F-130** Usage pages: task (deps: PLT-F-129)

### B14 — Trace and graph

13. S71 **PLT-F-131** Model fingerprint (deps: PLT-F-121, PLT-F-127)
14. S72 **PLT-F-132** Pipeline graph: chat on named nodes (deps: PLT-F-124, PLT-F-119, PLT-F-125, PLT-F-126, PLT-F-131) **gate it**
15. S73 **PLT-F-133** Pipeline graph: jobs on the graph (deps: PLT-F-132)
16. S74 **PLT-F-134** Step trace (deps: PLT-F-132, PLT-F-127)

### B15 — Clients, UI and requirement files

17. S75 **PLT-F-135** TypeScript client library (deps: PLT-F-125, PLT-F-122, PLT-F-119, PLT-F-123, PLT-F-131) **gate it**
18. S76 **PLT-C-022** npm release workflow for the TypeScript library (deps: PLT-F-135) **gate it**  ∥  S77 **PLT-F-136** Admin UI: per-model options and caps on grants (deps: PLT-F-117, PLT-F-125)  ∥  S78 **PLT-F-137** Admin UI: rule caps for keep-alive and thinking (deps: PLT-F-118); run the three together only if none adds a component to `web/shared`
19. S79 **PLT-F-138** Consumer page: options and caps (deps: PLT-F-125)  ∥  S80 **PLT-C-023** Real-Ollama contract tests, second set (deps: PLT-F-126, PLT-F-131)
20. S81 **PLT-C-024** Portal requirement file (deps: PLT-F-135, PLT-C-022, PLT-F-122, PLT-F-131)  ∥  S82 **PLT-C-025** personal-assistant requirement file update (deps: PLT-F-135, PLT-C-022, PLT-F-122, PLT-F-131); both are reviewed by the user and never committed from here

### B16 — Needs scoping

21. S83 **PLT-F-139** Host-side persistence of AI calls on client consent (deps: PLT-F-134); **blocked-on deferred Q-25**: the mechanism is not designed, so scoping comes first.

Owed from the user: set up the npm trusted publisher and the package scope (PLT-C-022). The defaults marked proposed in the log (`policy.task.max_length` 64, `usage.task.max_per_window` 50, `gateway.model_info_ttl` 5m, `scheduler.keep_alive_max` 1h, `reasoning_effort` values `none`, `low`, `medium`, `high`) are confirmed by the builder of each ticket. Parked, not tickets: model classes (D-08), the audio seam (D-16) and a thinking budget (D-38).
