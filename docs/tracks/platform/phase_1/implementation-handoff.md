# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-160).

## Last session (2026-11-15, M2 build 1)

- Built, with `make web-admin-test` passing (admin 97 tests, shared 46): PLT-F-032 admin shell and dashboard (`1f84f97`), PLT-F-033 consumers, invites and keys pages (`79064fa`), PLT-F-034 grants, models and policy rules pages (`5e1a33e`). Pages are one file each in `web/admin/src/pages/` (order 10-19 F-033, 20-29 F-034, 30-39 for F-035; requests page path must be `/requests`).
- D-160 (user's choice): PLT-F-027 and PLT-F-028 moved from M3 into M2, so PLT-F-035's requests page is built against a real backend. M3 is now PLT-F-036 and PLT-F-037 only.
- Ungated: no code gate has run over these commits. One gate at the end of M2 over `05b04f2..HEAD` (D-143).
- Close-out was done inline in one pass over all three tickets (not per ticket) to stay inside the budget. `implementation-status.md` is stale on purpose (rebuilt at the milestone end, finish plan rule 4), so `closeout-check` and `ticket-drift` still report status-mismatch for F-032..F-034.
- Spend this chat: about 0.8 window; running total 1.7 of 16.

## Waiting on you

- **Push** `main` so CI runs (M0 done only on green CI on three runners, D-158). Deferred by you.
- **M1 second-machine check** (`ops/checks/m1-checklist.md`): deferred by you. PLT-F-038 stays building until it passes.

## Next steps (M2, in order)

1. PLT-F-027 consumer accounts (invites, password and passkey login, sessions, consent), then PLT-F-028 resource requests (backend, approve writes a new grant version). Both Opus-sized; budget about 1 window each.
2. PLT-F-035: usage over time per consumer, audit log, settings, requests page and pending count (the F-032 badge already calls `/admin/v1/resource-requests` and hides on 501).
3. Playwright test of finish-line statements 2 and 5 (D-152) with a CI web job running `make web-admin-test` on Node 24 (`.github/workflows/ci.yml`, shared contract); lower the usage rollup interval in the test.
4. M2 gate over `05b04f2..HEAD`, then M3 (PLT-F-036, PLT-F-037).

Drift: 8 `stale-decision` warnings D-41..D-47, D-50 (docs-drift); `ops/bin/ai-mirror` and `sdlc-skills-install` are absent. `lld.md` module map lacks `webui` (M5 reconcile).
