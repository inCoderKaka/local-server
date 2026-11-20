# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-163).

## Last session (2026-11-20, live, M5 slice 1)

- Built: PLT-C-004 `1683cfe` (user guide Host setup, README install steps for `v0.1.0-rc.1`, ROADMAP B10 built and B8 wording after the green CI run). Docs only, no code gate (D-143).
- Second machine is a **Windows** consumer. `ops/checks/m1-checklist.md` updated for it (Git Bash runs `consumer-api-check`, admin login through the admin UI on 8486); new `ops/checks/m3-checklist.md` (`7aedfa0`). The user deferred both checks; they do not block M5.
- Decided (`8550851`): D-161 client integration with the interactive-learning-portal is designed now and built in phase 2; D-162 engine options the OpenAI API lacks go in one namespaced body object; D-163 the portal reaches the host through its own server-side route, D-148 (no CORS) stays.
- Spend 0.15 window (Opus main chat, $4.40).

## Open items

- **Portal integration design** (req_001 D-161) is `req_002` in this phase. On 2026-11-21 its stages 0 (ingest), 1 (discussion, closed D-19) and 2 (HLD, D-20..D-25) ran and verified; `design-status.md` records the resume point. **Next:** `/orchestrate-design platform 2 1`, which starts `design-lld`, then `design-ai-arch` (applies, narrow), `design-close`, `ticketize`. Tickets are phase 2 work (D-161); M5's developer guide waits on `design-close` to mark it planned.
- Seen in the portal repo (not this repo's work, never edited from here): its `local` provider calls from the browser (blocked by D-148), its capability table marks `top_k` and penalties as supported (local-server answers `unsupported_parameter`), and its default `localUrl` is `http://localhost:8080` (local-server's API is 8484).
- **M2+M3 gate:** fixes `5305511..2002da6` are committed, but the gate's final report never arrived; "gated" for F-027..F-037 is inferred from commits. Worklog has no gate rows for the M2+M3 and M4 gates.
- PLT-F-038 stays `building` until the M1 second-machine check.
- docs-drift: 12 known stale-decision warnings (D-41..D-47, D-50, D-141..D-144); `ticket-sizes.md` header totals; `ROADMAP.md:4` points at `next-steps.md`.

## Waiting on you

1. **M1 and M3 second-machine checks** on the Windows machine (`ops/checks/m1-checklist.md`, then `ops/checks/m3-checklist.md`). Say "checks done" in a chat and the host side is read (usage rows, `tailnet request` log lines, consent, session, request and audit rows).
2. Push `main` (4 new commits), then push `v0.1.0-rc.1`; run `ops/checks/macos-launchagent-checklist.md`; install on a clean macOS user account; then push `v0.1.0` (D-156).
3. **Rulings** (parking lines): (a) system-mode service runs the user-owned binary as root; recommended `--run-as "$(id -un)"` or user mode only for rc.1; (b) the user service is registered before the first run; recommended register after it.
4. **I-455:** is `/usage` plus the in-process e2e harness enough for statement 5? Recommended yes.

## Next steps

M5, in order: the portal integration design (D-161, a design chat), then the rest of the user guide (its "planned" banner and sections now built), HLD and LLD reconciled with the code (the LLD module map lacks `webui`), the developer guide (marks the portal integration as planned), `/phase-retro platform 1`.
