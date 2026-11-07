# platform / phase_1 — previous session closeout

Session: 2026-11-07 · live · 46c90d8..3f23862 · tree clean · 139 unpushed commits · migrations: not available (the repo names no migration-status command; 4 are written and applied to no real data dir)

**Open items**
- Code gate owed over PLT-B-053: `05bf043..46015cc`. It changes `api/openapi.yaml` and the regenerated `internal/api`. Deferred for budget (about 90% of the window used, ceiling 95%).
- Docs gate owed over `638f76b..HEAD`, plus this session's close-out commits. Deferred for budget. The session's commits are ungated.
- `implementation-status.md` was patched by hand at the end (PLT-C-019 row and counts), not regenerated.
- Not started: PLT-B-054, PLT-F-097, PLT-F-027 (now unblocked) and PLT-F-028.
- D-132's audit-row half is my reading of your answer (no audit row for an identical PUT); open to correction.

**Next steps**
- Build PLT-B-054, then the code gate over it together with `46015cc`.
- Then PLT-F-097 with PLT-F-027 in parallel, then PLT-F-028.
- Waiting on you: confirm the D-132 audit row; skim D-109..D-130 (written from summaries, best-effort depends-on); review I-387..I-394; say when to apply the four migrations.

**Where we stand:** phase_1 · batch B4 · S25, S26 (B-053), S26b, S27, S28 built; S26c (PLT-F-097), S29 (PLT-F-027), S30 (PLT-F-028) not started. 35 of 104 features done. Built this session: PLT-B-053 (`46015cc`); gate fixes `05bf043` closed PLT-B-032, B-058, B-059; PLT-B-030 and B-060 closed by status fixes; PLT-C-018 (rulings) and PLT-C-019 (the code gate) done.

| | Done: gated | Done: ungated | Open at start | New this session | Open now |
|---|---|---|---|---|---|
| **Batch B4** big tickets | 0 | 0 | 2 | 1 | 3 |
| small tickets | 0 | 0 | 4 | 4 | 8 |
| bugs | 2 | 2 | 7 | 5 | 8 |
| **Phase 1** big tickets | 0 | 1 | 18 | 2 | 19 |
| small tickets | 0 | 1 | 54 | 5 | 58 |
| bugs | 4 | 2 | 35 | 5 | 34 |

Dropped: 0. The agent's table was written before PLT-C-019 existed; the big-ticket phase row is hand-adjusted for it. Ungated: PLT-B-053, B-060, C-018, C-019.

**Bugs seen**
- PLT-B-061 identical rule PUT writes an audit row (gate, captured).
- PLT-B-062 deprecated `quota` with `window_seconds` applied at the default window with only a warning (captured).
- PLT-B-063 settings, models, login and password bodies carry no `warnings` (captured).
- PLT-B-064 `TestErrorEnvelopeUnknownField` name no longer fits (captured).
- PLT-B-065 `AccessKey.warnings` has no spec example (captured).
- Ideas captured: PLT-F-102..F-104, PLT-C-016, PLT-C-017.

**Verification**: track-work (worklog rows for PLT-B-053, PLT-B-060, PLT-C-018, PLT-C-019, gate row) · sync-docs (closeout-check exit 0, 6 of 6) · ticket-drift ok (38 warnings) · ai-mirror --check not available · naming-check exit 0 · docs-drift exit 0 (39 warnings) · budget (measured $26.11 = 89.9% of the calibrated window, ceiling 95% = $27.58) · gate (built 1: gated 0, ungated 1, owed: PLT-B-053 code gate, session docs gate) · docs drift (fixed: gate-owed wording, B-060 worklog row, D-41..D-45 depends-on path, PLT-C-019; still open: D-46, D-47, D-50 staleness, D-41..D-45 body text path, 31 schema-table-missing warnings)

**Decisions**
- Needs ruling: none.
- Added this session (new, all unreviewed, PLT-B-053, `46015cc`): I-387 design middleware walks the raw body by reflection · I-388 design `Invite` carries `warnings` · I-389 design service warnings before ignored-field warnings · I-390 design walker runs on every admin operation with a body · I-391 design one warning per ignored field, no dedup or cap · I-392 design names with U+FFFD or non-printable are never echoed · I-393 copy warning text `<path> is not a known field and was ignored` · I-394 scope unreachable `unexpectedField` regexp left in place.
- Last session's 25 listed rows I-358..I-386 were accepted this session. Older unreviewed design and scope rows, newest first: I-357, I-356, I-355, I-354, I-353, I-352, I-351, I-350, I-349, I-348, I-347, I-346, and about 320 more in `implicit-decisions.md`.
- 16 older unreviewed `copy` rows: see `implicit-decisions.md`.
