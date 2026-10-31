# platform / phase_1 — implementation status

Instance of the `implementation-status.md` format (built by `docs/tools/sdlc-workflow-skills/agents/implementation-status-reporter.md`). Requirement: `requirements/req_001.md` · log: `requirements/req_001.decisions.md` · ADR 0001, 0002, 0003, 0004, 0005. Last updated 2026-10-31.

## Position + next action
- **Position:** req_001 is the phase's only requirement; 90 ticket rows are tracked: 63 features (PLT-F-001..PLT-F-063), 11 chores and 16 bugs. Done (13): design close-out PLT-C-005 and PLT-C-007 (docs), PLT-C-001 (`37c2603`, gate fixes `d588ee1`), PLT-C-002 (legal drafts, not in force until the owner approves; `3cf4f3f`), and PLT-F-001..PLT-F-003 (`f00fa8a`, `6839035`, `ef86df3`; set B gate `2da4671`), PLT-F-004, PLT-F-006, PLT-F-007 (`5cd11e2`) and PLT-F-005, PLT-F-008, PLT-F-009 (`939208f`), the last six covered by the set C gate (`fab7890`). B1 is complete except spin-off PLT-F-053; B2 slices S7..S10 are done. 54 features are `idea` (PLT-F-054 and PLT-F-055 are `needs-decision` gate spin-offs), 5 chores are `idea` and 2 `designed` (PLT-C-003, PLT-C-004). Bugs: PLT-B-002 `fixed` (`2da4671`), 15 `reported` (PLT-B-001, PLT-B-003..PLT-B-016; PLT-B-007..PLT-B-016 each need a decision). PLT-C-001's CI has never run on GitHub (worklog). 42 commits are unpushed (`git log origin/main..HEAD`); the working tree is clean. req_001 has no open question (Open and Deferred are empty in its log), so it is not blocked (D-17). Pre-build confirmations that remain: PLT-F-018 SLO approval as a D-NN row (D-55, before its gate); PLT-B-001 needs a D-row next to D-62 before PLT-C-003.
- **Next action:** B2 S11 **PLT-F-010** (access keys; Sonnet; depends on PLT-F-007, PLT-F-008, PLT-F-009, all done; gate it), per `next-steps.md` item 7, which runs it in parallel with S12 **PLT-F-011** (policy engine; Opus; gate it) and S13 **PLT-F-012** (sharing state; Sonnet); B2 S14 **PLT-F-013** (host metrics; dependencies done) can fill a free builder slot.

## Progress table
| Batch | Slice | Ticket | Status | Owner | Model | Gate-covered |
|---|---|---|---|---|---|---|
| — | — | PLT-C-005 | done | Claude | Sonnet | yes (6520214, docs only) |
| — | — | PLT-C-007 | done | Claude | — | yes (d986f37 and earlier docs gates) |
| B1 | S1 | PLT-C-001 | done | Claude | Sonnet | yes (d588ee1, gate set A) |
| B1 | S1 | PLT-B-001 | reported | Claude | — | no |
| B1 | S2 | PLT-F-001 | done | Claude | Opus | yes (2da4671, set B gate) |
| B1 | S3 | PLT-F-002 | done | Claude | Sonnet | yes (2da4671, set B gate) |
| B1 | S4 | PLT-F-003 | done | Claude | Sonnet | yes (2da4671, set B gate) |
| B1 | S4 | PLT-F-053 | idea | Claude | Sonnet | no |
| B1 | S4 | PLT-B-002 | fixed | Claude | Sonnet | yes (2da4671, set B gate) |
| B1 | S5 | PLT-F-004 | done | Claude | Sonnet | yes (fab7890, set C gate) |
| B1 | S6 | PLT-F-005 | done | Claude | Sonnet | yes (fab7890, set C gate) |
| B2 | S7 | PLT-F-006 | done | Claude | Sonnet | yes (fab7890, set C gate) |
| B2 | S8 | PLT-F-007 | done | Claude | Sonnet | yes (fab7890, set C gate) |
| B2 | S9 | PLT-F-008 | done | Claude | Sonnet | yes (fab7890, set C gate) |
| B2 | S10 | PLT-F-009 | done | Claude | Sonnet | yes (fab7890, set C gate) |
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
| — | — | PLT-F-054 | idea (needs-decision) | Claude | Sonnet | no |
| — | — | PLT-F-055 | idea (needs-decision) | Claude | Sonnet | no |
| — | — | PLT-F-056, PLT-F-057, PLT-F-058, PLT-F-059, PLT-F-060 | idea | Claude | Sonnet | no |
| — | — | PLT-F-061, PLT-F-062, PLT-F-063 | idea | Claude | Sonnet | no |
| — | — | PLT-C-011 | idea | Claude | Sonnet | no |
| — | — | PLT-B-007, PLT-B-008, PLT-B-009, PLT-B-010, PLT-B-011, PLT-B-012 | reported (needs-decision) | — | — | no |
| — | — | PLT-B-013, PLT-B-014, PLT-B-015, PLT-B-016 | reported (needs-decision) | — | — | no |

## Gate ledger
- Last review gate: fab7890 (`fix(platform): review-gate fixes for 5cd11e2..939208f (set C, PLT-F-004..F-009; ...)`), a code gate over PLT-F-004..PLT-F-009 (18 actions applied, 0 skipped); it deferred PLT-B-013..PLT-B-016. Since fab7890: 1 commit, `0f182c8` (`docs(`: close-out step 2 and set C gate; no `feat(` or `fix(`). Uncommitted paths: 0. Earlier gate runs: 2da4671 (set B, PLT-F-001..F-003, PLT-B-002; deferred PLT-B-007..PLT-B-012, PLT-F-054, PLT-F-055), d588ee1 (set A, PLT-C-001/002), d986f37, def3ea8, 7bcd3c7 (docs fixes), 6520214 (design close-out and ticketization, 29 approved findings plus the glossary Module row).
- Shared contracts touched since the last gate (`git diff --name-only fab7890..HEAD`): none. The changed paths are `docs/modules/{audit,events,host,telemetry}.md` and the phase's tracking files (`backlog.md`, `bugs.md`, `implicit-decisions.md`, `next-steps.md`, `worklog.md`); no ticket schema or format spec, `ops/bin/**`, port, migration, public API route or installed skill/agent.
- Next gate: not due — the only commit since fab7890 is docs, and no shared contract changed. The next code gate falls due once the next `feat(` commits land (PLT-F-010 and PLT-F-011 are tagged "gate it").

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
- `bugs.md` holds twelve rows: PLT-B-002 `fixed`, the other eleven (PLT-B-001, PLT-B-003..PLT-B-012) `reported`. `ops/bin/ticket-drift` does not exist in this repo, so no automated drift check ran.
- PLT-B-002 — fixed `2da4671` (P2, build): archcheck does not guard the telemetry adapter (`internal/telemetry/slogprom`, `prometheus/client_golang`); found in PLT-F-003. (now fixed)
- PLT-B-003 — reported (P2, build): ogen's default error handler does not write the OpenAI error envelope and decode errors can quote request bytes; PLT-F-018 and PLT-F-024 must install the handlers.
- PLT-B-004 — reported (P2, build): the generated SSE response path does not flush; PLT-F-019 needs a flushing writer.
- PLT-B-005 — reported (P1, build): one ogen `Server` carries both `/v1` and `/admin/v1`; each listener must mount only its own prefix (PLT-F-024).
- PLT-B-006 — reported (P3, build): upstream ogen v1.24.0 panics on any `x-ogen-type` extension; the spec avoids it.
- PLT-F-052, PLT-F-053, PLT-C-009, PLT-C-010 — spin-offs (Source `build`, idea 2026-10-31, P3) from PLT-F-001..003. PLT-F-053 carries plan position P1/B1/S4 and PLT-F-052 P1/B3 (no slice); none is listed in `next-steps.md`. Not named as a next action.
- uncaptured — set B code gate over PLT-F-001, PLT-F-002, PLT-F-003 is owed (backlog Notes and worklog); the three rows are in `## Done but not shipped` with Code Rev. and Sec. Rev. `false`. (now stale: the gate ran, `2da4671`; the three backlog rows still say "code gate owed (set B)" with Code Rev. and Sec. Rev. `false`, and `next-steps.md` item 2 still says "set B code gate owed"; the status table counts them gate-covered from the worklog)
- uncaptured — `next-steps.md` item 2 is marked done for S2..S4 but the three tickets are `done`, not `shipped`, and their Sec. Rev. is still `false`; PLT-F-001 and PLT-F-002 are tagged "gate it" in `next-steps.md`.
- PLT-B-007 — reported (P2, review-gate): streamed chat completions require an `LS-Receipt` response header, contradicting D-39 F1 step 8 and D-27; needs-decision on the contract shape.
- PLT-B-008 — reported (P2, review-gate): remote admin login over the tailnet cannot work (`Secure` session cookie on an `http` admin server); needs-decision (HTTPS with Tailscale certificates vs dropping `Secure`).
- PLT-B-009 — reported (P2, review-gate): `GET /v1/jobs/{job_id}/result` deletes on read, so a lost response loses the result (against D-26); needs-decision.
- PLT-B-010 — reported (P2, review-gate): storage sets `synchronous(NORMAL)` under WAL, so a power loss can drop committed batches after the receipt was issued; needs-decision (`FULL` vs `NORMAL`).
- PLT-B-011 — reported (P3, review-gate): `Tx.Capture` always writes `syn_change`, though D-41 makes change-row sync opt-in and nothing prunes the rows; needs-decision.
- PLT-B-012 — reported (P2, review-gate): a failing log sink drops every log line silently (weakens D-39 step 9); needs-decision on how the host admin is told.
- PLT-F-054, PLT-F-055 — spin-offs (Source `review-gate`, idea 2026-10-31, P3, `needs-decision`): Idempotency-Key on resource-request creation; audit of failed admin logins. No plan position and not in `next-steps.md`. Not named as a next action.
- uncaptured — `next-steps.md` items 3 (PLT-F-004) and 5 (PLT-F-006, PLT-F-007) are not marked done though the tickets are `done` (`5cd11e2`, `d6538e3`); item 3 and the early-starter note still read as pending. (now fixed: next-steps marks items 3 to 6 done, `0f182c8`)
- PLT-F-056, PLT-F-057, PLT-F-058, PLT-F-059, PLT-F-060, PLT-C-011 — spin-offs (Source `build`, idea 2026-10-31, from PLT-F-004, PLT-F-006, PLT-F-007): no plan position (`—` Ref cells) and none listed in `next-steps.md`. PLT-C-011 (P2) is the live Ollama contract run that confirms the `overflowMarkers` texts and the 0.12.6 minimum (D-48); PLT-F-059 is needs-scoping. Not named as a next action.
- uncaptured — `implicit-decisions.md` now holds I-041..I-072 added by PLT-F-004, PLT-F-006, PLT-F-007 (per `d6538e3`), awaiting owner review. (now: it also holds I-073..I-089 added by PLT-F-005, PLT-F-008, PLT-F-009 per `0f182c8`; all awaiting owner review)
- uncaptured — PLT-B-007..PLT-B-012 and PLT-F-054/055 have no plan position and no Model in their rows, and `next-steps.md` does not list them.
- PLT-B-013, PLT-B-014, PLT-B-015, PLT-B-016 — reported (P2, P2, P2, P3; review-gate set C `5cd11e2..939208f`), each needs-decision: recovery code is shown once and cannot be re-shown (PLT-B-013); a locked Linux Secret Service prompt blocks startup forever (PLT-B-014); `ntf_event` has no retention rule (PLT-B-015); the `Notifier` name is claimed by both `internal/notifier/events` and the LLD's channel-adapter port (PLT-B-016). No plan position, no Model, not in `next-steps.md`.
- PLT-F-061, PLT-F-062, PLT-F-063 — spin-offs (Source `build`, idea 2026-10-31, P3, from PLT-F-009, PLT-F-005, PLT-F-008): no plan position and not in `next-steps.md`; PLT-F-061 is needs-scoping and PLT-F-062 overlaps the PLT-B-013 decision. Not named as a next action.
- uncaptured — `next-steps.md` "Early starters" still lists PLT-F-006, which is `done`.
- `bugs.md` now holds sixteen rows: PLT-B-002 `fixed`, the other fifteen (PLT-B-001, PLT-B-003..PLT-B-016) `reported`. The earlier bullet counting twelve is superseded. `ops/bin/ticket-drift` does not exist in this repo, so no automated drift check ran.

## Token ledger
| Ticket | Model | Input | Output | Cache (write / read) | Attribution |
|---|---|---|---|---|---|
| — | — | n/a | n/a | n/a | n/a — the token-ledger script found no transcript span or subagent whose prompt, assistant text or Bash commands name a phase ticket id; nothing attributable |

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
