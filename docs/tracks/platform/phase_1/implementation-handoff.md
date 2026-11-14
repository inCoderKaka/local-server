# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-160).

## Last session (2026-11-15, M2 build 1)

- Built, with `make web-admin-test` passing (admin 97 tests, shared 46): PLT-F-032 admin shell and dashboard (`1f84f97`), PLT-F-033 consumers, invites and keys pages (`79064fa`), PLT-F-034 grants, models and policy rules pages (`5e1a33e`). Pages are one file each in `web/admin/src/pages/` (order 10-19 F-033, 20-29 F-034, 30-39 for F-035; requests page path must be `/requests`).
- D-160 (user's choice): PLT-F-027 and PLT-F-028 moved from M3 into M2, so PLT-F-035's requests page is built against a real backend. M3 is now PLT-F-036 and PLT-F-037 only.
- Ungated: no code gate has run over these commits. One gate at the end of M2 over `05b04f2..HEAD` (D-143).
- Close-out was done inline in one pass over all three tickets (not per ticket) to stay inside the budget. `implementation-status.md` is stale on purpose (rebuilt at the milestone end, finish plan rule 4), so `closeout-check` and `ticket-drift` still report status-mismatch for F-032..F-034.
- Spend this chat: about 0.8 window; running total 1.7 of 16.

## Also built after the first close-out (same day)

- PLT-F-035 partial `d9542e2` (usage over time per consumer, audit log, settings pages) and the Playwright e2e for statements 2 and 5 plus the CI `web-admin` job and `make web-admin-e2e` (`65c5387`). e2e passed locally (1 passed); the CI job has never run, so it needs the push. Admin tests 114, shared 46. Still ungated; next gate is the M2 gate over `05b04f2..HEAD`.

## Waiting on you

- **Rule on I-455 / two judgement calls (needs-ruling):** (a) does `/usage` alone meet statement 5 ("current usage and usage over time per consumer")? Recommended yes; dashboard per-consumer usage is parked. (b) the e2e uses an in-process harness, not the shipped binary (no non-interactive key path exists; real-binary first run is the M1 checklist's job). Recommended accept.

- **Push** `main` so CI runs (M0 done only on green CI on three runners, D-158). Deferred by you.
- **M1 second-machine check** (`ops/checks/m1-checklist.md`): deferred by you. PLT-F-038 stays building until it passes.

## Next steps (M2, in order)

1. PLT-F-027 consumer accounts (invites, password and passkey login, sessions, consent), then PLT-F-028 resource requests (backend, approve writes a new grant version). Both Opus-sized; budget about 1 window each.
2. PLT-F-035 requests page only (approve, reject, comment; the F-032 badge already calls `/admin/v1/resource-requests` and hides on 501). Usage, audit and settings pages are done.
3. Extend the e2e when F-027/F-028 land (statement 4's live test is M3).
4. M2 gate over `05b04f2..HEAD`, then M3 (PLT-F-036, PLT-F-037).

Drift: 8 `stale-decision` warnings D-41..D-47, D-50 (docs-drift); `ops/bin/ai-mirror` and `sdlc-skills-install` are absent. `lld.md` module map lacks `webui` (M5 reconcile).
