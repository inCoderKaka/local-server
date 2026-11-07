# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session.

## Last session

- 2026-11-07, passive, `f696e97..HEAD`. PLT-B-054 built (`6aad23c`) and gated (`bc26451`); it settles PLT-B-035. The code gate over `05bf043..6aad23c` also closed PLT-B-053's owed gate. The re-plan found nothing eligible under the default priority.
- Tree clean; commits unpushed (see the closeout header). One worktree (`main`).
- Migrations: not available (the repo names no migration-status command). Four are written and applied to no real data dir: `grants` 0002 and 0003, `identity` 0002, `usage` 0004.

## Leftovers

- **Ungated, small:** the fix commit after `bc26451` that made the `ResourceRequest.warnings` description valid YAML and regenerated `internal/api` (a comment-only change; build, vet, vacuum and the api and admin tests pass). It has no code gate of its own; fold it into the next code gate.
- **Docs gate:** the session-end docs gate over `638f76b..HEAD` is run by the closing steps of this session. Check the closeout for its result.
- **Unbuilt, in order:**
  1. PLT-F-097 (B4/S26c, D-106; owns the `identity` 0003 cascade migration).
  2. PLT-F-027 (B4/S29). No D-row records the PLT-C-002 draft approval or the `go-webauthn` dependency.
  3. PLT-F-028 (B4/S30), after F-027. It must fill `ResourceRequest.warnings` through the shared walker.
- **Ruled but not built:** PLT-B-061 (D-132), the D-112..D-121 rulings (B-028, B-031, B-041, B-026, B-029, B-038, B-040, B-047, B-048, B-052) and the P3 ones. Their ticket rows do not point at the D-rows yet. A "bugs only" priority makes PLT-B-031, B-038, B-040, B-052, B-047 and B-029 eligible for a passive session.
- **Captured and open from this session:** PLT-B-066 (the `prompt_tokens_estimated` marker is signed into a dropped receipt and not stored; needs `usage` migration 0005), PLT-B-067 (needs ruling), PLT-F-105, PLT-F-106.
- **Drift not fixed:** D-46, D-47 and D-50 stale against their depends-on; the D-41..D-45 body paths; 31 `schema-table-missing` warnings; 18 `WARN-no-model` and 19 `WARN-source-no-origin` ticket warnings.
- CI has never run on GitHub. The Windows sleep-lock adapter has never run on Windows. `ai-mirror` is not available in this repo.

## Waiting on you

- **PLT-B-067 ruling.** A job cut short by the host stopping bills an estimated prompt, then becomes `failed(host_restart)`; the consumer resubmits and pays twice. (a) Bill only the completion chunks received and keep the estimate for a consumer cancel and a job deadline (recommended), or (b) bill the estimate in every aborted case (the code today). I-395 is tied to the answer.
- **PLT-F-105 ruling.** Does a call that fails midway bill an estimated prompt under D-108? Recommended: yes, behind `gateway.chars_per_token`, decided together with PLT-B-067.
- **PLT-B-066:** your go to apply `usage` migration 0005. A passive session cannot. Recommended: apply it with the four pending migrations in one back-up-first run.
- **D-132's audit row** (blocks PLT-B-061). Recorded as no audit row for an identical PUT. Confirm or correct.
- Skim D-109..D-130 in `req_001.decisions.md` (written from summaries; `depends-on` cells are best-effort).
- Review I-387..I-402. Recommended: accept. Look first at I-387, I-388, I-392, I-399 (marker outside the canonical leaf) and I-402.
- Say when to apply the four pending migrations to a real data dir (back up first).
- Needing a live go: PLT-F-041, F-044, F-045, F-070.

## Next steps

- Build PLT-F-097, then gate it with the next set.
- Then PLT-F-027 with PLT-F-028 after it, each set gated.
