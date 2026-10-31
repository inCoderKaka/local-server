# platform / phase_1 — implementation status

Instance of the `implementation-status.md` format (built by `docs/tools/sdlc-workflow-skills/agents/implementation-status-reporter.md`). Requirement: `requirements/req_001.md` · log: `requirements/req_001.decisions.md` · ADR 0001, 0002, 0003, 0004, 0005. Last updated 2026-10-31.

## Position + next action
- **Position:** req_001 is the phase's only requirement; 69 ticket rows are tracked: 53 features (PLT-F-001..PLT-F-053), 10 chores and 6 bugs. Done (7): design close-out PLT-C-005 and PLT-C-007 (docs), PLT-C-001 (Go scaffold; `37c2603`, gate fixes `d588ee1`), PLT-C-002 (legal drafts, not in force until the owner approves; `3cf4f3f`), and the first three features, all built 2026-10-31 and awaiting their code gate (set B): PLT-F-001 OpenAPI contract and ogen server (`f00fa8a`), PLT-F-002 storage foundation (`6839035`), PLT-F-003 telemetry port (`ef86df3`). 50 features are `idea` (47 of them designed; PLT-F-049..053 are spin-offs), 4 chores are `idea` and 2 `designed` (PLT-C-003, PLT-C-004). PLT-C-001's CI has never run on GitHub (worklog). 31 commits are unpushed (`git log origin/main..HEAD`), and 9 paths are uncommitted (docs and track files only). req_001 has no open question (Open and Deferred are empty in its log), so it is not blocked (D-17). Pre-build confirmations that remain: PLT-F-018 SLO approval as a D-NN row (D-55, before its gate); PLT-B-001 needs a D-row next to D-62 before PLT-C-003. PLT-F-001's ogen coverage spike (D-57) was done in its build (worklog).
- **Next action:** per `next-steps.md`, B1/S5 **PLT-F-004** (runtime settings with owners, plus retention registry; Sonnet, depends only on PLT-F-002, done; gate it), then B1/S6 **PLT-F-005**. The review gate (set B) over `f00fa8a..ef86df3` is owed and due before or alongside PLT-F-004. PLT-F-006 (B2/S7) can fill a free builder slot early.

## Progress table
| Batch | Slice | Ticket | Status | Owner | Model | Gate-covered |
|---|---|---|---|---|---|---|
| — | — | PLT-C-005 | done | Claude | Sonnet | yes (6520214, docs only) |
| — | — | PLT-C-007 | done | Claude | — | yes (d986f37 and earlier docs gates) |
| B1 | S1 | PLT-C-001 | done | Claude | Sonnet | yes (d588ee1, gate set A) |
| B1 | S1 | PLT-B-001 | reported | Claude | — | no |
| B1 | S2 | PLT-F-001 | done | Claude | Opus | no (code gate owed, set B) |
| B1 | S3 | PLT-F-002 | done | Claude | Sonnet | no (code gate owed, set B) |
| B1 | S4 | PLT-F-003 | done | Claude | Sonnet | no (code gate owed, set B) |
| B1 | S4 | PLT-F-053 | idea | Claude | Sonnet | no |
| B1 | S4 | PLT-B-002 | reported | Claude | Sonnet | no |
| B1 | S5 | PLT-F-004 | idea (designed) | Claude | Sonnet | no |
| B1 | S6 | PLT-F-005 | idea (designed) | Claude | Sonnet | no |
| B2 | S7 | PLT-F-006 | idea (designed) | Claude | Sonnet | no |
| B2 | S8 | PLT-F-007 | idea (designed) | Claude | Sonnet | no |
| B2 | S9 | PLT-F-008 | idea (designed) | Claude | Sonnet | no |
| B2 | S10 | PLT-F-009 | idea (designed) | Claude | Sonnet | no |
| B2 | S11 | PLT-F-010 | idea (designed) | Claude | Sonnet | no |
| B2 | S12 | PLT-F-011 | idea (designed) | Claude | Opus | no |
| B2 | S13 | PLT-F-012 | idea (designed) | Claude | Sonnet | no |
| B2 | S14 | PLT-F-013 | idea (designed) | Claude | Sonnet | no |
| B3 | S15 | PLT-F-014 | idea (designed) | Claude | Sonnet | no |
| B3 | S16 | PLT-F-015 | idea (designed) | Claude | Sonnet | no |
| B3 | S17 | PLT-F-016 | idea (designed) | Claude | Sonnet | no |
| B3 | S18 | PLT-F-017 | idea (designed) | Claude | Sonnet | no |
| B3 | S19 | PLT-F-018 | idea (designed) | Claude | Sonnet | no |
| B3 | S20 | PLT-F-019 | idea (designed) | Claude | Sonnet | no |
| B3 | S21 | PLT-F-020 | idea (designed) | Claude | Sonnet | no |
| B3 | S22 | PLT-F-021 | idea (designed) | Claude | Sonnet | no |
| B3 | S23 | PLT-F-022 | idea (designed) | Claude | Sonnet | no |
| B3 | S24 | PLT-F-023 | idea (designed) | Claude | Sonnet | no |
| B3 | S19 | PLT-B-003 | reported | Claude | Sonnet | no |
| B3 | S20 | PLT-B-004 | reported | Claude | Sonnet | no |
| B3 | — | PLT-F-052 | idea | Claude | Sonnet | no |
| B4 | S25 | PLT-F-024 | idea (designed) | Claude | Sonnet | no |
| B4 | S25 | PLT-B-005 | reported (P1) | Claude | Sonnet | no |
| B4 | S26 | PLT-F-025 | idea (designed) | Claude | Sonnet | no |
| B4 | S27 | PLT-F-026 | idea (designed) | Claude | Sonnet | no |
| B4 | S28 | PLT-C-002 | done | Claude | Opus | yes (d588ee1, gate set A) |
| B4 | S29 | PLT-F-027 | idea (designed) | Claude | Sonnet | no |
| B4 | S30 | PLT-F-028 | idea (designed) | Claude | Sonnet | no |
| B5 | S31 | PLT-F-029 | idea (designed) | Claude | Sonnet | no |
| B5 | S32 | PLT-F-030 | idea (designed) | Claude | Sonnet | no |
| B5 | S33 | PLT-F-031 | idea (designed) | Claude | Sonnet | no |
| B6 | S34 | PLT-F-032 | idea (designed) | Claude | Opus | no |
| B6 | S35 | PLT-F-033 | idea (designed) | Claude | Opus | no |
| B6 | S36 | PLT-F-034 | idea (designed) | Claude | Opus | no |
| B6 | S37 | PLT-F-035 | idea (designed) | Claude | Opus | no |
| B6 | S38 | PLT-F-036 | idea (designed) | Claude | Opus | no |
| B6 | S39 | PLT-F-037 | idea (designed) | Claude | Opus | no |
| B7 | S40 | PLT-F-038 | idea (designed) | Claude | Sonnet | no |
| B7 | S41 | PLT-F-039 | idea (designed) | Claude | Sonnet | no |
| B7 | S42 | PLT-F-040 | idea (designed) | Claude | Sonnet | no |
| B8 | S43 | PLT-F-041 | idea (designed) | Claude | Sonnet | no |
| B8 | S44 | PLT-F-042 | idea (designed) | Claude | Sonnet | no |
| B8 | S45 | PLT-C-003 | designed | Claude | Sonnet | no |
| B8 | S46 | PLT-F-043 | idea (designed) | Claude | Sonnet | no |
| B9 | S47 | PLT-F-044 | idea (designed) | Claude | Sonnet | no |
| B9 | S48 | PLT-F-045 | idea (designed) | Claude | Sonnet | no |
| B9 | S49 | PLT-F-046 | idea (designed) | Claude | Sonnet | no |
| B9 | S50 | PLT-F-047 | idea (designed) | Claude | Sonnet | no |
| B9 | S51 | PLT-F-048 | idea (designed) | Claude | Opus | no |
| B10 | S52 | PLT-C-004 | designed | Claude | Sonnet | no |
| B4 | — | PLT-F-050 | idea | Claude | Haiku | no |
| B4 | — | PLT-F-051 | idea (needs-scoping) | Claude | Sonnet | no |
| — | — | PLT-F-049 | idea | Claude | Sonnet | no |
| — | — | PLT-C-006 | idea | — | — | no |
| — | — | PLT-C-008 | idea | Claude | — | no |
| — | — | PLT-B-006 | reported (upstream ogen, P3) | — | — | no |
| — | — | PLT-C-009 | idea | Claude | — | no |
| — | — | PLT-C-010 | idea | Claude | — | no |

## Gate ledger
- Last review gate: d588ee1 (`fix(platform): review-gate fixes for 7454922..3cf4f3f (set A, PLT-C-001; req_001 D-21)`), a code and docs gate over PLT-C-001 (`37c2603`) and PLT-C-002 (`3cf4f3f`); it left PLT-B-001 deferred. Since d588ee1: 4 commits: `f00fa8a` feat PLT-F-001, `6839035` feat PLT-F-002, `ef86df3` feat PLT-F-003, `9853112` docs close-out of PLT-C-001/002; the three feat commits are the owed set B gate. Uncommitted paths: 9 (docs and track files only: ROADMAP.md, docs/design_docs/lld.md, docs/developer-guide.md, docs/glossary.md, and the phase's backlog, bugs, implicit-decisions, next-steps and worklog). Earlier gate runs: d986f37, def3ea8, 7bcd3c7 (docs fixes), 6520214 (design close-out and ticketization, 29 approved findings plus the glossary Module row).
- Shared contracts touched since the last gate (`git diff --name-only d588ee1..HEAD`): public API contract `api/openapi.yaml` and the generated `internal/api/**` (PLT-F-001); migrations and the writer/unit-of-work in `internal/storage/**` (`migrations/0001_ops_tables.sql`, `0002_syn_change.sql`, PLT-F-002); telemetry port `internal/telemetry/**` (PLT-F-003); `go.mod`/`go.sum` (tool directives, new dependencies); module notes `docs/modules/{api,storage,telemetry}.md` and `docs/schema-reference.md`. Not touched: `docs/tracks/README.md`, `.github/**`, `.claude/**`, `docs/tools/sdlc-workflow-skills/**`. (`ops/bin/**` does not exist in this repo.)
- Next gate: due — three `feat(` commits (PLT-F-001, PLT-F-002, PLT-F-003) and shared contracts (OpenAPI spec, migration layout and unit of work) changed with no gate over them; their backlog rows say "code gate owed (set B)".

## Drift & bugs inbox
- uncaptured — `design-status.md` still says req_001 stage 6 (`ticketize`) is "not started" and "Next: run `ticketize platform 1 1`", but 52 tickets exist (6e8f4eb); its stage 6 row and Position need updating. (now fixed: stage 6 is `done`, verified 2026-10-30)
- uncaptured — PLT-F-006 Notes say "Can start as soon as T01 is done" and PLT-F-045 Notes say "Can start once T06 is done": T01/T06 are stale draft ticket numbers, not ids in this backlog (T01 presumably means PLT-C-001; T06 cannot be resolved, and PLT-F-045's Depends On is PLT-F-002 and PLT-F-005). (now fixed: the Notes name PLT-C-001 and PLT-F-005)
- uncaptured — `design-status.md` predates 2e5e2bd: its stage 3 row lists D-38 as a live LLD row (D-54 supersedes it) and omits D-53..D-56; its Artifacts line omits ADR 0004 and `execetive_inputs/26-10-30_platform_req_001_pre-implementation.md` (design.md already carries D-53..D-56).
- uncaptured — PLT-F-016 Notes say which key signs ledger checkpoints is undecided (D-27 (4) covers receipts only) and a D-NN row is needed before building, but the log's Open section is empty, so the D-17 gate does not see it. (now fixed: D-65 decides the checkpoint key; PLT-F-016 Notes updated)
- uncaptured — PLT-F-030 Notes say the default for consumer webhook endpoints targeting private (RFC 1918, ULA) or tailnet addresses must be confirmed by the user before the ticket builds; no log row records the question. (now fixed: D-66 sets the default to deny; PLT-F-030 updated)
- uncaptured — `worklog.md` has one row (PLT-C-005, Ref up to 6e8f4eb): the review-gate fixes (6520214), D-51/D-52 (fdea028), the verbatim session record (f7da149) and D-53..D-56 with ADR 0004 (2e5e2bd) are not logged, and the PLT-C-005 row does not name the gate that ran over it. (now fixed: the worklog has backfilled rows for 6520214..2e5e2bd and for PLT-C-007)
- PLT-F-049 — spin-off (Source `build`, idea 2026-10-31, P3): commercial-ready transport. Its Ref cell has no `P1/B/S` plan position and `next-steps.md` does not list it; not named as a next action. (now captured as PLT-F-049)
- PLT-C-006 — spin-off (Source `build`, idea 2026-10-31, P3): model-licence review for paid hosting, billing phase. No plan position and not in `next-steps.md`.
- uncaptured — PLT-C-007 (done) has no plan position in its Notes and its worklog rows say "review-gate (docs) pending" on the first row, while later rows record the gates; the status table shows it gate-covered by the docs gates through d986f37.
- PLT-B-001 — reported (P2, review-gate): CI actions in `.github/workflows/ci.yml` and `fuzz.yml` use floating `@v7` tags with no SHA pins or update bot; needs a D-row next to D-62 (recommended: SHA-pin + dependabot, before PLT-C-003).
- PLT-F-050, PLT-F-051 — spin-offs (Source `build`, idea 2026-10-31, from PLT-C-002): Ref has plan position `P1/B4` but no slice, and `next-steps.md` does not list them; PLT-F-051 is needs-scoping (an open owner question on re-acceptance of new Terms/Privacy versions). Not named as a next action.
- PLT-C-008 — spin-off (Source `build`, idea 2026-10-31, P3): fuzz-failure issue opener; depends on PLT-C-001 (done) but has no plan position and is not in `next-steps.md`.
- uncaptured — `implicit-decisions.md` is untracked and holds 15 `unreviewed` AI choices (I-001..I-015) awaiting owner review; PLT-C-002's drafts carry about 10 open legal points that the owner must settle before PLT-F-027 (`next-steps.md`). (now tracked and modified: it holds I-001..I-040, with I-016..I-040 added by PLT-F-001..003; the added ones are also awaiting owner review)
- uncaptured — PLT-C-001's CI workflows (`ci.yml`, `fuzz.yml`) have never run on GitHub (worklog row); the 3-OS matrix, race job and govulncheck are verified only locally.
- `bugs.md` holds six open rows (PLT-B-001..PLT-B-006, all `reported`). `ops/bin/ticket-drift` does not exist in this repo, so no automated drift check ran.
- PLT-B-002 — reported (P2, build): archcheck does not guard the telemetry adapter (`internal/telemetry/slogprom`, `prometheus/client_golang`); found in PLT-F-003.
- PLT-B-003 — reported (P2, build): ogen's default error handler does not write the OpenAI error envelope and decode errors can quote request bytes; PLT-F-018 and PLT-F-024 must install the handlers.
- PLT-B-004 — reported (P2, build): the generated SSE response path does not flush; PLT-F-019 needs a flushing writer.
- PLT-B-005 — reported (P1, build): one ogen `Server` carries both `/v1` and `/admin/v1`; each listener must mount only its own prefix (PLT-F-024).
- PLT-B-006 — reported (P3, build): upstream ogen v1.24.0 panics on any `x-ogen-type` extension; the spec avoids it.
- PLT-F-052, PLT-F-053, PLT-C-009, PLT-C-010 — spin-offs (Source `build`, idea 2026-10-31, P3) from PLT-F-001..003. PLT-F-053 carries plan position P1/B1/S4 and PLT-F-052 P1/B3 (no slice); none is listed in `next-steps.md`. Not named as a next action.
- uncaptured — set B code gate over PLT-F-001, PLT-F-002, PLT-F-003 is owed (backlog Notes and worklog); the three rows are in `## Done but not shipped` with Code Rev. and Sec. Rev. `false`.
- uncaptured — `next-steps.md` item 2 is marked done for S2..S4 but the three tickets are `done`, not `shipped`, and their Sec. Rev. is still `false`; PLT-F-001 and PLT-F-002 are tagged "gate it" in `next-steps.md`.

## Token ledger
| Ticket | Model | Input | Output | Cache (write / read) | Attribution |
|---|---|---|---|---|---|
| — | — | n/a | n/a | n/a | n/a — the token-ledger script found no transcript span or subagent naming a phase ticket id; nothing attributable |

## Requirement coverage

### req_001 → ticket
| Requirement line | Covered by |
|---|---|
| This directory should be a new git repository 'local-server' in GitHub account 'inCoderKaka' | no ticket, decision only (D-18) |
| Created under an open source licence | no ticket, decision only (D-18) |
| Purpose: turn my localhost into a server any application can use for AI work | PLT-F-001, PLT-F-018 (D-02, D-32) |
| Build an app from scratch with which any person can turn their PC into a server of resources others can use | PLT-F-043, PLT-F-027 (D-02) |
| A user can either use resources hosted by others or host their own machine's resources | PLT-F-027, PLT-F-043 (D-02) |
| The application should be very lightweight | PLT-C-001 (D-09, D-21) |
| The host can tune the amount of resources they share | PLT-F-014, PLT-F-024 (D-26) |
| The host can create API keys for other users to consume the host's resources | PLT-F-010, PLT-F-025, PLT-F-033 (D-25) |
| The host can monitor any user's usage with good granularity over time | PLT-F-023, PLT-F-035 (D-27, D-40) |
| The host can monitor current usage as well as usage over time | PLT-F-023, PLT-F-013, PLT-F-032 (D-31, D-40) |
| Every page shows resources used by other apps and resources reserved by this app for sharing | PLT-F-013, PLT-F-024, PLT-F-032 (D-31) |
| Done over very secure, encrypted channels | PLT-F-038, PLT-F-039, PLT-F-040 (D-12, D-23, D-24) |
| Main purpose: let my friend with a Mac mini share resources with any user of PA | PLT-F-038, PLT-F-039 (D-03) |
| PA must be updated too; create PA's requirement file at personal-assistant `docs/tracks/platform/phase_2/requirements/req_001.md` | no ticket, decision only (D-03, D-04, D-17) |
| Refer to the PA project; both apps should be well compatible | PLT-F-001 (D-03, D-32) |
| Everything as dynamically configurable as possible | PLT-F-004, PLT-F-024 (D-21, D-25, D-38) |
| The whole software well extensible for potential future features | PLT-C-001 (D-02) |
| The host can load huge AI models (as they allow) into memory, used for AI calls by consumers they've allowed | PLT-F-006, PLT-F-014, PLT-F-026 (D-22) |
| The host can set the context window length limit, etc. | PLT-F-006, PLT-F-026, PLT-F-034 (D-22) |
| Communication very easy: consumers ask the host for more resources; the host approves or rejects; with comments etc. | PLT-F-028, PLT-F-029, PLT-F-035, PLT-F-037 (D-29, D-32, D-38, D-39) |
| Create a glossary (host user vs. user user confusion) | no ticket, docs/glossary.md (D-46) |
| §1 Summary: small app on the host machine in front of a local model runtime; authenticated, encrypted, metered access; admin UI to set limits, issue keys, watch usage, approve/reject requests; PA first client — ME: "exactly" | PLT-F-018, PLT-F-024, PLT-F-028 (D-02, D-21) |
| §2 Glossary: Host, Host machine/Node, Consumer, Client app, Access key, Share budget, Grant, Resource request, Engine — ME: "yes, good enough" | no ticket, docs/glossary.md (D-46) |
| §3 Feasible › choose allowed models, preload/unload, keep in memory | PLT-F-014, PLT-F-026 (D-22) |
| §3 Feasible › context-length cap per model and per key | PLT-F-006, PLT-F-026 (D-22) |
| §3 Feasible › max output tokens, concurrency, rate limits, token quotas per key | PLT-F-014, PLT-F-017, PLT-F-026 (D-25, D-26) |
| §3 Feasible › usage per request, rolled up over time | PLT-F-015, PLT-F-023 (D-27) |
| §3 Feasible › show "used by other apps vs. reserved for sharing" on every page | PLT-F-013, PLT-F-032 (D-31) |
| §3 Feasible › keys created, revoked, set to expire | PLT-F-010, PLT-F-025 (D-25, D-38) |
| §3 Feasible › request, approve/reject and comment flow | PLT-F-028, PLT-F-035, PLT-F-037 (D-32, D-38, D-39) |
| §3 Not enforceable on macOS (hard RAM/CPU/GPU caps; GPU reading needs sudo) › indirect budget enforcement — ME: ignore for now | PLT-F-014, PLT-F-013 (D-26, D-31) |
| ME: make it OS agnostic | PLT-C-001, PLT-F-041, PLT-F-042, PLT-C-003 (D-30, D-31) |
| ME: easily installable via brew, winget, or git clone + add to PATH | PLT-C-003, PLT-F-043, PLT-C-004 (D-04 (2): brew and winget moved to a later phase; git clone + build from source in PLT-C-004; D-30) |
| §3 Reachability: Mac mini behind a home router, PA prod on Fly; options Tailscale / Cloudflare Tunnel / port forwarding / own relay; recommendation: transport adapter, default Tailscale | PLT-F-038, PLT-F-039 (D-12, D-23, D-24) |
| ME: go with the recommendation, but no compromise on security; Cloudflare ruled out unless we can secure it ourselves | PLT-F-038, PLT-F-039 (D-12, D-23, D-24) |
| ME: build for a possible future own relay server | PLT-C-001 (D-02, D-23, D-24) |
| Building block › don't write our own inference; wrap an engine via an adapter; Ollama default, MLX/llama.cpp later | PLT-F-006 (D-22) |
| Building block › single Go binary, SQLite, admin web UI in the binary on localhost only; no Electron; only the consumer API is reachable | PLT-C-001, PLT-F-002, PLT-F-032, PLT-F-038 (D-09, D-10, D-21, D-28) |
| Building block › OpenAI-compatible API (/v1/chat/completions, /v1/embeddings, /v1/models) + /v1/capabilities | PLT-F-001, PLT-F-018, PLT-F-019, PLT-F-021 (D-32, D-39) |
| Security › keys stored only as hashes, scoped to models, with expiry and revocation | PLT-F-010, PLT-F-025 (D-25) |
| Security › prompt/response content not logged by default | PLT-F-003, PLT-F-022 (D-31, D-40) |
| Security › consumers can only run inference, never arbitrary code | PLT-F-018 (D-25) |
| Not in Phase 1 › general compute sharing; public marketplace — ME: recommendation good | no ticket, decision only (D-02) |
| ME: build so a future public marketplace can find any host | no ticket, decision only (D-02) |
| Phase 1 = private network where the host invites known people | PLT-F-027, PLT-F-038 (D-13, D-23, D-25) |
| A1: Phase 1 shares only inference (chat/completions + embeddings) — accepted | PLT-F-018 (D-02) |
| A2 (corrected): host invites in Phase 1; build for public discovery and a central server in future | no ticket, decision only (D-02) |
| A3 (corrected): build for Apple and Windows in Phase 1 | PLT-F-041, PLT-C-003, PLT-F-043 (D-30) |
| A4 (corrected): consumer can have an account; individual or application user; or just an API key | PLT-F-025, PLT-F-027 (D-13, D-25) |
| A5: resource-request flow is part of local-server's consumer API; PA gets a UI for it — accepted | PLT-F-028, PLT-F-037 (D-32, D-39) |
| A6: usage per request (key, model, tokens in/out, latency, queue wait, status); rollups per minute 7 d, per hour 90 d, per day forever — accepted | PLT-F-015, PLT-F-023 (D-16, D-27, D-44) |
| Q1: all PA environments (local dev and Fly production) use the host machine | PLT-F-038, PLT-F-039 (D-12, D-23, D-24) |
| Q2: Cloudflare must not read plaintext; acceptable if we encrypt end to end ourselves; behind an adapter; Tailscale for now | PLT-F-038, PLT-F-039 (D-12, D-23, D-24) |
| Q3: as generic as possible, but build only inference now, for PA | PLT-F-001 (D-02) |
| Q4: eventually consumers browse hosts; initially private invites only | no ticket, decision only (D-02) |
| Q5 › PA: don't queue non-background requests; use a fallback when the host is offline | no ticket, decision only (D-03, D-11) |
| Q5 › background requests may be queued; users set preferences for how they're notified (incoming requests, limit-increase requests, etc.) | PLT-F-022, PLT-F-029, PLT-F-035, PLT-F-037 (D-11, D-26, D-29, D-39) |
| Q5 › if the host declines, the consumer uses a fallback if configured, else waits for the host | no ticket, decision only (D-03, D-11) |
| Q6: stack criteria: simplest/lightweight, easy to download and use, extensible (future servers possible); website local first, later pluggable to hosted servers | PLT-C-001, PLT-F-002, PLT-F-032 (D-09, D-21, D-28) |
| Q7: host seeing prompts/responses is a future capability; legally correct; data-sharing contracts later; costable; extensible, scalable; best HLD/LLD decisions | PLT-F-015, PLT-F-046, PLT-F-047 (D-27, D-41, D-40) |
| Q8: build for the possibility of hosts charging consumers (ledger-grade usage) | PLT-F-015, PLT-F-016 (D-02, D-16, D-27, D-41) |
| Q9: licence — explain each later | no ticket, decision only (D-18) |
| Q10: PA req file: whole context of this app, compatibility standards with other apps, focus on PA integration, refer to this repo's req files, written from local-server's perspective too | no ticket, decision only (D-17, D-32) |
| Q11: how PA plugs in — discuss later | no ticket, decision only (D-03) |
| Q12: don't create the GitHub repo; the task was the formal req file | no ticket, decision only (D-18) |
| Q13: Linux support in Phase 1 — okay | PLT-F-041, PLT-C-003, PLT-F-043 (D-04, D-30) |
| Q14: installer installs / detects the AI engine (Ollama) — "Sure" | PLT-F-043 (D-14, D-30) |
| Q15: background service at login/boot (launchd, Windows service); prevent sleep while sharing — sure | PLT-F-041, PLT-F-042 (D-30) |
| Q16: auto-update as a background option offered at UI startup when not enabled, the prompt disappearing soon; curl install commands also good | no ticket, decision only (D-04 (3): auto-update moved to a later phase, overriding the round-2 answer 16; D-30) |
| Q17: no code signing yet | PLT-C-003 (D-30) |
| Q18: remote management — whatever's cheapest to implement now, scoped for remote management | PLT-F-038 (D-10) |
| Q19: consumers get their own web page (log in, usage, limits, requests) — yes | PLT-F-036, PLT-F-037 (D-04, D-10, D-32) |
| Q20: consumer accounts created both by the host and by invite-link sign-up (login method not answered) | PLT-F-025, PLT-F-027 (D-13, D-25) |
| Q21: notification channels in Phase 1 — all (in-app, desktop/OS, webhooks, email via the host's settings, Telegram) | PLT-F-029, PLT-F-030, PLT-F-031 (D-04 (1): email and Telegram moved to a later phase, overriding the round-2 answer "21: all"; D-29) |
| Q22: host and consumer can make different request types (request properties/tags) defining what the host machine does, within what's allowed for the consumer | PLT-F-011, PLT-F-026, PLT-F-034 (D-15, D-25, D-26, D-33) |
| Q23: signed vs. tamper-evident usage records — not understood; ask in a future chat | PLT-F-015, PLT-F-016 (D-16, D-27) |
| Q24: just write the (PA) file; don't commit for now | no ticket, decision only (D-17) |
| (added) Host status endpoint `/v1/health` (online, loaded models, queue depth) | PLT-F-021 (D-05) |
| (added) Pause-sharing switch + quiet hours | PLT-F-012, PLT-F-032, PLT-F-042 (D-06) |
| (added) Audit log of admin actions | PLT-F-008, PLT-F-035 (D-07) |
| (added) Records survive loss of the host disk: host-encrypted off-host sync (host-owned bucket snapshots by default, opt-in platform sync to Supabase; D-41), adapter, migration traces, export/import tooling | PLT-F-002, PLT-F-044, PLT-F-045, PLT-F-046 (D-41, D-44) |
| (added) Platform admin role can see a host's data per dataset, time-boxed, only on that host's approval | PLT-F-046, PLT-F-047, PLT-F-048 (D-43, D-44) |
| (added) Future-scoping direction (docs/reqs/phase1/req001_future_scoping.txt): categorized users for subscriptions, consented data and profiles, host benchmarking and smart host selection, party-to-party trace sharing, decentralized by default, classifiable and billable data, legal decision traces | PLT-C-001, PLT-F-007, PLT-F-021, PLT-F-047 (D-41, D-42, D-43, D-44, D-45) |
