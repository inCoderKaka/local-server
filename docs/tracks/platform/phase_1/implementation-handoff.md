# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session.

## Last session

- 2026-11-08, passive. It finished the set A code gate over `ee41663..d6f7d6c` (PLT-C-020) for PLT-F-097, PLT-F-041 and PLT-F-044. The fixes are `d6f7d6c` and `8fea837`; the gate's close-out is `47bf594`. Nothing new was built; the token budget ran out after the gate.
- Tree clean; commits unpushed (see the closeout header). One worktree (`main`).
- Migrations: not available (the repo names no migration-status command). Four are written and applied to no real data dir: `grants` 0002 and 0003, `identity` 0002, `usage` 0004. PLT-F-097 added none (D-136).

## Leftovers

- **Ungated:** the gate's own fix commits `d6f7d6c` and `8fea837` (33 non-test files, incl. the new `backup.s3.abort_timeout` setting and `Store.BoolTx`). Fold them into PLT-F-045's code gate.
- **Docs gate owed:** the session docs gate over `7378f87..HEAD` was not run (budget). Run it with the next session's docs gate.
- **PLT-B-084:** approved by the gate, not implemented (restore opens the live database before `refuseIfRunning`; the first fix broke `TestRestoreRefusesWithoutTheRightCodeAndWritesNothing`).
- **Unbuilt, in order:** PLT-F-045 (B9/S48, gate it). Then PLT-F-027 (blocked on the PLT-C-002 drafts) and PLT-F-028 after it.
- **Ruled but not built:** the D-112..D-121 rulings (PLT-B-028, B-031, B-041, B-026, B-029, B-038, B-040, B-047, B-048, B-052) and PLT-B-061 (D-132).
- **Captured and open:** PLT-B-066, B-067, PLT-F-105, F-106 (aborted-call billing); PLT-B-068..076, PLT-F-110, F-111 (from F-041 and F-044); PLT-B-077..083, PLT-F-112, F-113 (from this gate).
- **Drift still flagged:** 8 `stale-decision` warnings (D-41..D-47, D-50): D-133 reaffirmed them, and the 2026-11-08 edits to `glossary.md` and `privacy-consent.md` came after it. PLT-F-112's Ref says `P1/B9/S46`, which is wrong (its parent PLT-F-041 is B8/S43); choose the slice. The Depends-on cells of D-46, D-47 and D-50 write `execetive_inputs/...` without the `docs/` prefix. `ai-mirror` and `sdlc-skills-install` are not in this repo.
- CI has never run on GitHub. The Windows service adapters and the sleep lock have never run on Windows. PLT-F-044's real-bucket test (PLT-B-076) has never run.

## Waiting on you

- **Seven decisions from the gate (recommendation first):**
  - PLT-B-077: refuse a system-mode install when the binary or its folder is writable by a non-root user, and say how to install it under a root-owned path.
  - PLT-B-078: write system-mode logs under the data directory and give launchd a `StandardErrorPath` there, bounded by a size setting.
  - PLT-B-079: add a retention age of about 7 days beside the count, so the window is days, not hours.
  - PLT-B-080: allow plain HTTP only for loopback endpoints.
  - PLT-B-081: put the host id in the snapshot name before any user has backups.
  - PLT-B-082: create only the last folder and fail when the parent is missing.
  - PLT-B-083: raise the existing `backup.failed` notification with a prune reason.
- **PLT-B-067 and PLT-F-105**, together. Recommended: on a host stop bill only the completion chunks received; keep the estimate for a consumer cancel and a job deadline; F-105 yes, behind `gateway.chars_per_token`. I-395 is tied to the answer.
- **PLT-B-066:** your go to apply `usage` 0005 with the four pending migrations in one back-up-first run.
- **D-132's audit row** (blocks PLT-B-061): confirm "no audit row for an identical PUT".
- **PLT-C-002 Terms and Privacy drafts** (block PLT-F-027): approve them. D-135 already allows `go-webauthn`. PLT-B-042 also needs your ok for `golang.org/x/sys`.
- **D-41..D-47 and D-50:** say whether the 2026-11-08 edits to `glossary.md` and `privacy-consent.md` change them.
- **PLT-F-113** needs scoping (CLI or admin API; no two runs at once). Give PLT-F-112 a slice.
- Review I-403..I-445 (recommended: accept all). Look first at I-431, I-432, I-425, I-436, I-438 and I-444.
- Skim D-109..D-130 and D-134..D-140.
- Say when to apply the pending migrations to a real data dir (back up first).
- Needing a live go: PLT-F-045, F-070.

## Next steps

- Build PLT-F-045 (B9/S48) and gate it together with `d6f7d6c` and `8fea837`.
- Then PLT-F-027 and PLT-F-028 once the PLT-C-002 drafts are approved.
