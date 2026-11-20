# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-163).

## Last session (2026-11-21, design chat, req_002)

- Designed and ticketized req_002 (portal integration and more tunable request parameters): LLD D-26..D-30, AI architecture D-31..D-38, design-close D-39..D-41 (ADR 0006..0008, HLD 6.14, 6.15 and 8.6, the LLD req_002 sections, glossary), then ticketize: PLT-F-114..139 and PLT-C-021..025 in B11..B16 of `next-steps.md`. **Build phase 2:** req_001 D-164 and req_002 D-42 lift the freeze for these tickets only; they are outside the finish line and the budget. No code changed, no gate (D-143).
- Dropped PLT-C-004's stale dependency on PLT-F-039 (public mode is phase 2).
- Q-25 (host-side persistence on client consent) is deferred and blocks only PLT-F-139.

## Earlier session (2026-11-20, live, M5 slice 1)

- Built: PLT-C-004 `1683cfe` (user guide Host setup, README install steps for `v0.1.0-rc.1`, ROADMAP B10 built and B8 wording after the green CI run). Docs only, no code gate (D-143).
- Second machine is a **Windows** consumer. `ops/checks/m1-checklist.md` updated for it (Git Bash runs `consumer-api-check`, admin login through the admin UI on 8486); new `ops/checks/m3-checklist.md` (`7aedfa0`). The user deferred both checks; they do not block M5.
- Decided (`8550851`): D-161 client integration with the interactive-learning-portal is designed now and built in phase 2; D-162 engine options the OpenAI API lacks go in one namespaced body object; D-163 the portal reaches the host through its own server-side route, D-148 (no CORS) stays.
- Spend 0.15 window (Opus main chat, $4.40).

## Open items

- **Portal integration design** (req_001 D-161, `req_002`) is **done and ticketized** (see Last session). Its `@planned` public-docs blocks are owed: run `/design-close platform 2 1 topup`. The user confirms the proposed defaults when each ticket is built (`policy.task.max_length` 64, `usage.task.max_per_window` 50, `gateway.model_info_ttl` 5m, `scheduler.keep_alive_max` 1h). PLT-C-022 needs the npm trusted publisher and package scope set up by you.
- Seen in the portal repo (not this repo's work, never edited from here): its `local` provider calls from the browser (blocked by D-148), its capability table marks `top_k` and penalties as supported (local-server answers `unsupported_parameter`), and its default `localUrl` is `http://localhost:8080` (local-server's API is 8484).
- **M2+M3 gate:** fixes `5305511..2002da6` are committed, but the gate's final report never arrived; "gated" for F-027..F-037 is inferred from commits. Worklog has no gate rows for the M2+M3 and M4 gates.
- PLT-F-038 stays `building` until the M1 second-machine check.
- docs-drift: 12 known stale-decision warnings (D-41..D-47, D-50, D-141..D-144); `ticket-sizes.md` header totals; `ROADMAP.md:4` points at `next-steps.md`.

## Waiting on you

1. **M1 and M3 second-machine checks** on the Windows machine (`ops/checks/m1-checklist.md`, then `ops/checks/m3-checklist.md`). Say "checks done" in a chat and the host side is read (usage rows, `tailnet request` log lines, consent, session, request and audit rows).
2. Push `main` (32 new commits, all docs), then push `v0.1.0-rc.1`; run `ops/checks/macos-launchagent-checklist.md`; install on a clean macOS user account; then push `v0.1.0` (D-156).
3. **Rulings** (parking lines): (a) system-mode service runs the user-owned binary as root; recommended `--run-as "$(id -un)"` or user mode only for rc.1; (b) the user service is registered before the first run; recommended register after it.
4. **I-455:** is `/usage` plus the in-process e2e harness enough for statement 5? Recommended yes.

## Next steps

M5, in order: the rest of the user guide (its "planned" banner and sections now built), HLD and LLD reconciled with the code (the LLD module map lacks `webui`), the developer guide (marks the req_002 integration as planned), `/phase-retro platform 1`.
