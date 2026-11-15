# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-160).

## Last session (2026-11-15, M2 build 3, passive)

- Built and committed: PLT-F-027 step A `a5664a5` (invite sign-up, password login and logout, sessions, lockout, consent, `/v1/me*`, page-listener wiring; `internal/consumerauth/`), PLT-F-028 backend `05f3df1` (`internal/requests/`, 9 routes, atomic approve to a new grant version, TTL sweeper, shared walker `internal/api/ignored`), PLT-F-035 requests page `d8b4119` and the real-backend e2e `1cc0d70` (`make web-admin-e2e`: 2 passed, 0 failed, 0 skipped; admin 127 tests, shared 46). Statement 4's host half (approve and reject in the admin UI) now passes in a browser.
- Close-out `a85b3f4` plus this commit: worklog, backlog, sizes, glossary, user guide, decision rows I-456..I-463. F-028 re-sized to big.
- Ungated: no code gate has run over any M2 commit. Spend this chat about 0.7 window; running total 2.6 of 16.

## Waiting on you

- **Push** `main` so CI runs (M0 done only on green CI, D-158; the `web-admin` CI job has never run). Deferred by you.
- **M1 second-machine check** (`ops/checks/m1-checklist.md`). Deferred by you.
- **I-455 (needs-ruling):** does `/usage` alone meet statement 5, and is the in-process e2e harness acceptable? Recommended yes to both.

## Next steps (M2, in order)

1. PLT-F-027 step B: the 5 passkey routes with go-webauthn (D-135). Override the five 501 routes on `pageAPI`; add a `consumerauth.Adapter` and register it in `Service.adapters` (`service.go`). Budget about 0.4 window. Finish-plan cut 1 (passkeys) is available if the budget runs short; D-154 keeps add-passkey inside F-027.
2. M2 gate over `05b04f2..HEAD` (one gate, D-143; include the consumerauth security surface).
3. M3: PLT-F-036, PLT-F-037 (consumer web page). The e2e for statement 4 is M3.

Open known issues, all parked (no tickets, D-142): `TestUnknownFieldWithAnUnsafeNameIsNotEchoed` flaky about 1 in 140; `GrantVersion.resource_request_id` always null on reads (history "Made by" shows "The host"); purge leaves request text behind; `settingList` returns secret settings to a signed-in admin.

Drift: `ticket-drift` flags the stale `implementation-status.md` (rebuilt at the milestone end) and F-028 "done" while F-027 is in-progress (F-028 needs only step A); 8 `stale-decision` warnings D-41..D-47, D-50; `lld.md` module map lacks `webui` (M5).
