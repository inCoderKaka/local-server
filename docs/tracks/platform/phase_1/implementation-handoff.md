# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session.

## Last session

- 2026-11-06, semi-live, `638f76b..9a6134f`.
- Built, all ungated: PLT-F-096 (windowed quota list, `billing_mode`, persisted request counter), PLT-F-026 (admin API for grants, models and versioned policy rules), PLT-B-019, PLT-B-020, chores PLT-C-014 (`ops/bin` checkers) and PLT-C-015 (persisted ticket size map).
- Owner rulings on the seven `needs-ruling` rows are recorded as D-105 to D-108.
- Migrations: not available (the repo names no migration-status command). Four are written and applied to no real data dir: `grants` 0002 and 0003, `identity` 0002, `usage` 0004. Nothing was pushed.
- Budget: the session spent about $24 of a $27.58 ceiling (95% of the calibrated window, `docs/ops/plan-window.md`), so the review gates were deferred.

## Leftovers

- **Code gate owed** for the set PLT-F-096 + PLT-F-026 over `a0124e8^..79d0973` (94 files). It covers four migrations; `api/openapi.yaml` with the regenerated `internal/api`; `internal/limits`, `usage`, `grants`, `identity`, `auth`, `gateway`; `internal/admin` (consumers, grants); `cmd/local-server/wire.go`. It carries PLT-F-096, F-026, B-019, B-020. Run it before PLT-F-028 or PLT-F-027 consume the quota or policy shapes. Note `a0124e8` holds only the removal of the in-memory counter, so review it together with `400ee0b`.
- **Docs gate owed** over `638f76b..HEAD`, including `ops/bin` (PLT-C-014, `3879f98`), which is code.
- PLT-B-053 (tolerate unknown fields with a `warnings` list, D-105) and PLT-B-054 (estimated prompt tokens on aborted streams, D-108) are confirmed with builds pending. PLT-F-097 (purge of soft-deleted consumers, D-106) is designed. PLT-B-055 to B-060 are reported. PLT-F-098 to F-101 are ideas.
- Five fixable doc drifts: PLT-B-030, B-032 (settled by D-107) and B-035 (settled by D-108) are still `reported` in `bugs.md`; `docs/modules/gateway.md:5` calls `/v1/capabilities` planned (PLT-B-060); D-41 to D-45 in `req_001.decisions.md` cite a moved depends-on path.
- Older gate findings not ticketed: a `doctor` sleep-lock check; the relative invite link while `invites.link_base_url` is empty; consent `evidence` kept on consumer delete (now covered by D-106 and PLT-F-097); earlier `syn_change` payloads keep personal fields; `ConsumerRepo.SoftDelete` writes no audit row or event.
- CI has never run on GitHub. The Windows sleep-lock adapter has never run on Windows.

## Waiting on you

- Approve the PLT-C-002 legal drafts (recommended: approve after review). PLT-F-027 stays blocked without it.
- Rule on PLT-B-027 (recommended: `restore` reports and warns, no new mechanism in phase 1), PLT-F-052 and PLT-B-045 (D-105 probably settles it).
- Decide whether to ticket the older gate findings above (recommended: yes, as P2/P3 spin-offs after the gate).
- Review I-346 to I-386 (recommended: accept; look first at I-346, I-356, I-368 and I-378).
- Say when to apply the four migrations to a real data dir.
- Left out of passive sessions by the no-list, each needing a live go unless you widen it: PLT-F-041 (dependency), F-044, F-045 (security and privacy), F-070 (root daemon), F-027 (auth and consent).

## Next steps

- Run the owed code gate for the set PLT-F-096 + PLT-F-026.
- Build PLT-B-053, then PLT-B-054, then PLT-F-097, then PLT-F-028. PLT-F-027 after the legal drafts are approved.
