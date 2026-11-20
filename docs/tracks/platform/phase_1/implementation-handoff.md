# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-163).

## Last session (2026-11-21, live, M5 docs)

- All M5 doc tasks are done: HLD `b0c8128`, LLD `f856e07` (module map now has `webui`), user guide and README `3113169`, developer guide `f4858d7`. The req_002 design is closed (req_002 D-43, `0a72109`); I-455 is accepted. Docs only, no code gate (D-143); the session's docs gate ran once at the end.
- Glossary module count (29) and listener wording, `docs/modules/webui.md` and `offhost.md` were reconciled with the code; `ticket-sizes.md` now covers PLT-F-139 and PLT-C-025 (PLT-F-113 re-read as big).
- No ticket was built. Every commit is `docs(`. Migrations: not available (the repo names no migration-status command).

## Open items

- **PLT-C-022** (npm package for the portal integration) needs the npm trusted publisher and package scope set up by you. When each req_002 ticket is built you confirm its proposed defaults (`policy.task.max_length` 64, `usage.task.max_per_window` 50, `gateway.model_info_ttl` 5m, `scheduler.keep_alive_max` 1h).
- **Portal repo** (never edited from here): its `local` provider calls from the browser (blocked by D-148), its capability table marks `top_k` and penalties as supported (local-server answers `unsupported_parameter`), and its default `localUrl` is `http://localhost:8080` (local-server's API is 8484).
- **M2+M3 gate:** fixes `5305511..2002da6` are committed, but the gate's final report never arrived; "gated" for F-027..F-037 is inferred from commits. The worklog has no gate rows for the M2+M3 and M4 gates.
- PLT-F-038 stays `building` until the M1 second-machine check.
- docs-drift: 14 stale-decision warnings (req_001 D-41..D-47, D-50, D-141..D-144; req_002 D-02, D-10); `ROADMAP.md` line 3 points at `next-steps.md`.
- 9 findings from the doc reconciliation are one line each in `phase-2-parking.md` (code comments, spec text, port 8485 wording). No tickets (D-142).
- `c96069f` (a tests-only CI fix) has no gate over it. The recommendation is no gate.

## Waiting on you

1. **M1 and M3 second-machine checks** on the Windows machine (`ops/checks/m1-checklist.md`, then `ops/checks/m3-checklist.md`). Say "checks done" in a chat and the host side is read (usage rows, `tailnet request` log lines, consent, session, request and audit rows).
2. Push `main` (all commits since `origin/main` are docs), then push `v0.1.0-rc.1`; run `ops/checks/macos-launchagent-checklist.md`; install on a clean macOS user account; then push `v0.1.0` (D-156).
3. **Rulings** (parking lines): (a) the system-mode service runs the user-owned binary as root; recommended `--run-as "$(id -un)"` or user mode only for rc.1; (b) the user service is registered before the first run; recommended register after it.

## Next steps

`/phase-retro platform 1`, after finish-line statement 6 (a tagged release with checksums) passes. It runs in the main chat, not as a builder step. Nothing else is left in M5. Phase 2 (req_002 build) opens only after `v0.1.0`.
