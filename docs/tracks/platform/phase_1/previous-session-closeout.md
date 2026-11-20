# platform / phase_1 — previous session closeout

Session: 2026-11-21 · live · b9f031e..4b68d81 · tree clean · 42 unpushed commits · migrations: not available

**Open items**
- No code gate is owed (M5 is docs only, D-143). The session's one docs gate ran: 9 fixes committed in `4b68d81`, 17 candidates rejected, 1 declined, 0 deferred.
- `c96069f`, a tests-only CI fix, has no gate over it. Recommended: no gate.
- The M2+M3 gate's final report never arrived, so "gated" for F-027..F-037 is inferred from commits. The worklog has no gate rows for the M2+M3 and M4 gates.
- PLT-F-038 stays `building` until the M1 second-machine check.
- 9 findings from the HLD, LLD and guide reconciliation are one line each in `phase-2-parking.md`. No tickets (D-142).
- docs-drift has 14 stale-decision warnings. `ROADMAP.md` line 3 points at `next-steps.md`.
- The gate's note: I-456 ("no IP or browser data") conflicts with the privacy notice approved in D-153. Mark it superseded if you want.

**Next steps**
- `/phase-retro platform 1`, after finish-line statement 6 (a tagged release with checksums) passes. It runs in the main chat, not as a builder step. Nothing else is left in M5.
- Waiting on you: the M1 and M3 second-machine checks on the Windows machine; push `main`, then `v0.1.0-rc.1`, the macOS launch-agent checklist and clean-account install, then `v0.1.0`; the two rulings (system-mode service run-as, user service registered before the first run); PLT-C-022 npm trusted publisher and package scope.

**Where we stand**: phase_1 · batch B10 · slice S52: 1/1 tickets done; built this session: none (five M5 doc tasks without ids: HLD `b0c8128`, LLD `f856e07`, user guide and README `3113169`, developer guide `f4858d7`, req_002 design closed `0a72109`, D-43).

| | Done: gated | Done: ungated | Open at start | New this session | Open now |
|---|---|---|---|---|---|
| **Batch B10** big tickets | 0 | 0 | 0 | 0 | 0 |
| small tickets | 0 | 0 | 0 | 0 | 0 |
| bugs | 0 | 0 | 0 | 0 | 0 |
| **Phase 1** big tickets | 0 | 0 | 34 | 0 | 34 |
| small tickets | 0 | 0 | 66 | 0 | 66 |
| bugs | 0 | 0 | 41 | 0 | 41 |

Dropped: none. Ungated: none.

**Bugs seen**: none ticketed (D-142). 9 uncaptured lines in `phase-2-parking.md`: no admin password reset, the consumer page cannot create keys, CORS headers still sent, a per-model keep-alive not applied, stale doc comments and `go.mod` indirect mark, admin login failures not audited, the unmeasured HLD §4 estimates, the `api/openapi.yaml` text cleanup, and the port 8485 wording.

**Verification**: track-work (worklog rows for the M5 tasks, no ids) · sync-docs (closeout-check exit 0) · ticket-drift (exit 0, 0 warnings) · ai-mirror --check not available · naming-check (exit 0, 0 warnings) · drift audit (ran, no fixable drift left) · budget (not asked; counted 2.27M tokens, about 37% of the calibrated window) · gate (built 0: gated 0, ungated 0, owed: none; docs gate 9 fixed `4b68d81`) · docs drift (fixed: glossary module count and listener wording, webui and offhost module notes, ticket-sizes; still open: 14 stale-decision warnings, intended side unclear)

**Decisions**
- Needs-ruling: none. I-455 was accepted this session (`0a72109`).
- Added this session: none. The builders' choices were docs wording (D-14), so no I-NNN rows. The one logged decision is req_002 D-43 (design closed, no top-up owed).
- Older unreviewed design and scope rows, newest first (the 25 newest; I-480..I-454 as listed at the start of the session): I-480 installers default every prompt to no; I-479 `install` on an existing binary behaves as `upgrade`; I-478 uninstall asks about the data folder; I-477 first run judged from the `doctor` host line; I-476 install settings; I-475 Ollama and Tailscale offered after a yes; I-473 six bare binaries and `SHA256SUMS`; I-472 `-trimpath`, no `-s -w`; I-471 release workflow has two jobs; I-469 Dependabot weekly, github-actions only; I-468 CI actions on v7, pinned; I-467 passkey per-address limit setting; I-466 no list or delete passkey routes; I-465 passkey UV and resident key `required`; I-464 passkey ceremony state in memory; I-463 Requests page nav order and state; I-462 audit rows only for approve and reject; I-461 new `requests.*` settings; I-460 ignored-field walker moved to `internal/api/ignored`; I-459 no audit rows for consumer sign-up or login; I-458 invite refusal codes; I-457 consumer lockout reuses host settings; I-456 consent purposes `terms` and `privacy`; I-454 settings page infers type, unit and secret from the key name; I-453 usage page defaults to hour buckets over 24 hours.
- And 398 more in `implicit-decisions.md`; 19 older unreviewed `copy` rows are there too.
