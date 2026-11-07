# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session.

## Last session

- 2026-11-07, live, `46c90d8..HEAD`. Closed: your rulings D-109..D-132 are recorded; the code gate over `a0124e8^..79d0973` ran and its fixes are in `05bf043`; PLT-B-053 is built (`46015cc`).
- 138 commits unpushed. One worktree (`main`), no stale ones.
- Migrations: not available (the repo names no migration-status command). Four are written and applied to no real data dir: `grants` 0002 and 0003, `identity` 0002, `usage` 0004.
- Budget: you asked for under 95% of the 5-hour window. The session ended near 90%, so both gates below were deferred. The commits they cover are **ungated**.
- `implementation-status.md` was patched by hand at the end (one chore row and the counts), not regenerated, to stay under the ceiling. The next report call regenerates it.

## Leftovers

- **Code gate owed** over `05bf043..46015cc` (PLT-B-053: `api/openapi.yaml`, regenerated `internal/api`, the admin handlers). Run it before anything consumes the admin response shapes. Fold PLT-B-054 into the same set if it is built first.
- **Docs gate owed** over `638f76b..HEAD` (includes `ops/bin`, which is code), plus this session's close-out commits.
- **Unbuilt, in order:**
  1. PLT-B-054 (B3/S20, D-108: estimated prompt tokens on an aborted stream).
  2. PLT-F-097 (B4/S26c, D-106: purge of soft-deleted consumers; owns the `identity` 0003 cascade migration, since `idn_consent` already exists in `identity` 0001).
  3. PLT-F-027 (B4/S29), now unblocked: you approved the PLT-C-002 drafts as working versions. Dependency `go-webauthn` allowed.
  4. PLT-F-028 (B4/S30), after F-027. It must call `withIgnored` on `ResourceRequestAsk`.
- **Ruled but not yet built:** PLT-B-061 (D-132), plus the P1/P2 rulings D-112..D-121 (B-028, B-031, B-041, B-026, B-029, B-038, B-040, B-047, B-048, B-052) and P3 ones; their ticket rows were not updated to point at the D-rows.
- **Captured and open:** PLT-B-061..B-065, PLT-F-102..F-104, PLT-C-016, PLT-C-017.
- **Drift not fixed:** D-46, D-47 and D-50 are stale against ADR, glossary and legal-draft changes; the body text of D-41..D-45 still says `docs/reqs/...` (the depends-on cells are fixed); 31 `schema-table-missing` warnings (PLT-B-044, PLT-C-009).
- CI has never run on GitHub. The Windows sleep-lock adapter has never run on Windows. `ai-mirror` is not available in this repo.

## Waiting on you

- **D-132's audit row** (blocks PLT-B-061). Your answer said "new version only if not identical". It is recorded as no audit row for an identical PUT too. Confirm or correct.
- The D-109..D-130 D-rows were written from the agent's summary of each ruling and their `depends-on` cells are best-effort. Skim them in `req_001.decisions.md`.
- Review I-387..I-394. Recommended: accept; look first at I-387 (middleware by reflection), I-388 and I-392.
- Say when to apply the four migrations to a real data dir (they run at `serve` start; back up the data dir first).
- Needing a live go: PLT-F-041 and F-044 (dependencies), F-045, F-070.

## Next steps

- Build PLT-B-054, then run the code gate over it together with `46015cc`.
- Then PLT-F-097 and PLT-F-027 (parallel), then PLT-F-028, each set gated.
