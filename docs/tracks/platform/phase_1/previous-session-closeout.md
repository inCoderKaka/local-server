# platform / phase_1 — previous session closeout

Session: 2026-11-08 · passive · 7378f87..0ba644b · tree clean · 168 unpushed commits · migrations: not available (the repo names no migration-status command; 4 are written and applied to no real data dir: `grants` 0002 and 0003, `identity` 0002, `usage` 0004; none added)

**Open items**
- The set A gate's own fixes (`d6f7d6c`, `8fea837`: 33 non-test files, incl. the new `backup.s3.abort_timeout` setting and `Store.BoolTx`) have no gate of their own. Fold them into PLT-F-045's code gate.
- The session docs gate over `7378f87..HEAD` was not run (token budget used up); it is owed.
- PLT-B-084 is approved but not implemented: offline `restore` opens the live database before `refuseIfRunning`; the fixer's change broke `TestRestoreRefusesWithoutTheRightCodeAndWritesNothing` and was reverted.
- PLT-F-044 is still untested against a real bucket and GCS/R2 (PLT-B-076). CI has never run on GitHub; the Windows adapters have never run on Windows.

**Next steps**
- Build PLT-F-045 (B9/S48) and gate it with the `d6f7d6c`/`8fea837` fixes. PLT-F-027 and PLT-F-028 wait on your PLT-C-002 approval.
- Waiting on you: seven decisions PLT-B-077..083; PLT-B-067 with PLT-F-105; `usage` 0005 and the pending migrations; D-132's audit row; PLT-C-002 drafts; D-41..D-47 and D-50 against the 2026-11-08 edits; scoping of PLT-F-113 and a slice for PLT-F-112; review I-403..I-445.

**Where we stand:** phase_1 · batch B9 · 38 of 113 features done; built this session: none. The session finished the interrupted set A code gate (PLT-C-020) over PLT-F-097, PLT-F-041 and PLT-F-044: 166 candidates, 50 approved, 49 implemented, 1 skipped, 14 deferred.

| | Done: gated | Done: ungated | Open at start | New this session | Open now |
|---|---|---|---|---|---|
| **Batch B9** big tickets | 0 | 0 | 4 | 1 | 5 |
| small tickets | 0 | 0 | 2 | 1 | 3 |
| bugs | 0 | 0 | 3 | 6 | 9 |
| **Phase 1** big tickets | 1 | 0 | 19 | 2 | 20 |
| small tickets | 0 | 0 | 60 | 1 | 61 |
| bugs | 0 | 0 | 40 | 8 | 48 |

Dropped: none. Gated: PLT-C-020 (the gate itself); PLT-F-097, F-041 and F-044 were already done at the base and are now covered by `8fea837`. Ungated: none built. The base counts were recomputed from `7378f87`, not carried from a plan call.

**Bugs seen**
- PLT-B-077: a system-mode service runs a binary a user may replace (captured, needs decision).
- PLT-B-078: system-mode logs under root's home; no launchd stderr path (captured, needs decision).
- PLT-B-079: default backup retention keeps about 8 hours (captured, needs decision).
- PLT-B-080: `http://` bucket endpoints accepted (captured, needs decision).
- PLT-B-081: prune deletes other hosts' snapshots in a shared folder or prefix (captured, needs decision).
- PLT-B-082: the folder target creates the whole path when the drive is unmounted (captured, needs decision).
- PLT-B-083: a prune failure is only logged (captured, needs decision).
- PLT-B-084: the restore running-host check comes late (captured).
- Ideas captured: PLT-F-112 (restarting or failed state in `service status`), PLT-F-113 (a "back up now" command).

**Verification**
- track-work: worklog rows for PLT-C-020, PLT-F-097, PLT-F-041, PLT-F-044 and the spin-offs · sync-docs (`closeout-check` exit 0, 6 of 6) · ticket-drift ok (0 warnings, 217 tickets) · ai-mirror --check not available · naming-check ok (0) · docs-drift exit 0 (8 warnings, known) · budget (counted 4,496,804 of 4,340,873 tokens = 72.5% of the calibrated window; limit 70%, so the session stopped after the gate) · gate (built 0; code gate `ee41663..d6f7d6c` fixed 49, skipped 1, deferred 14; docs gate not run, owed; owed code gate: `d6f7d6c`, `8fea837`) · docs drift (fixed: gated notes, PLT-B-027 text, size map, Source cells, 7 decision rows; still open: D-41..D-47 and D-50 stale against depends-on, PLT-F-112's Ref slice, decision-log path text)

**Decisions**
- Needs-ruling rows: none. PLT-B-067 (a bug) awaits your ruling and ties to I-395.
- Added this session (new, `unreviewed`):
  - I-445 (new) design: the purge audit field is `invite_refs_cleared`; the privacy notice states the 90-day removal.
  - I-444 (new) design: `backup-credentials` writes one redacted audit row; folder targets need an absolute path; leftover temp copies are swept at backup start.
  - I-443 (new) design: the minimize rule is read and parsed inside the write (`Store.BoolTx`).
  - I-442 (new) design: an empty bucket endpoint with region `auto` is refused.
  - I-441 (new) design: a second install with an unchanged definition returns Changed false with a note.
  - I-440 (new) design: user mode refused under sudo; other-mode install warning; systemd probe tells unknown from stopped.
  - I-439 (new) design: service name and control-character validation.
  - I-438 (new) design: multipart abort bounded by `backup.s3.abort_timeout` (default 1m).
- Older `unreviewed` design and scope rows (newest first, 25 shown): I-437, I-436, I-435, I-434, I-433, I-432, I-431, I-430, I-429, I-428, I-427, I-426, I-425, I-424, I-423, I-422, I-420, I-419, I-418, I-417, I-416, I-415, I-414, I-413; and the rest in `implicit-decisions.md`.
- 18 older `unreviewed` `copy` rows: see `implicit-decisions.md`.
