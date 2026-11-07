# platform / phase_1 — previous session closeout

Session: 2026-11-07 · passive · f696e97..c65cfae · tree clean · 148 unpushed commits · migrations: not available (the repo names no migration-status command; 4 are written and applied to no real data dir: `grants` 0002 and 0003, `identity` 0002, `usage` 0004)

**Open items**
- PLT-B-054 is built and gated, but a later one-line spec fix (`ee41663`, `ResourceRequest.warnings` description made valid YAML, `internal/api` regenerated) has no code gate of its own. It is comment-only; build, vet, vacuum and the api and admin tests pass. Fold it into the next code gate.
- PLT-B-067 awaits your ruling; PLT-B-066 needs `usage` migration 0005; PLT-F-105 needs a D-108 ruling; PLT-F-106 needs scoping.
- The docs gate left D-132's audit-row text, D-108's marker name and the PLT-C-002 / `go-webauthn` approvals (no D-row) for you.

**Next steps**
- Build PLT-F-097 (B4/S26c), then gate it; then PLT-F-027 and PLT-F-028 (each set gated). Nothing else was eligible for a passive session under the default priority.
- Waiting on you: rule PLT-B-067 and PLT-F-105; confirm D-132's audit row; review I-387..I-402; skim D-109..D-130; say when to apply the four pending migrations and `usage` 0005; the PLT-C-002 drafts and `go-webauthn` approvals; live go for PLT-F-041, F-044, F-045, F-070. With a "bugs only" priority, PLT-B-031, B-038, B-040, B-052, B-047 and B-029 become eligible.

**Where we stand:** phase_1 · batch B3 · S20 complete. 35 of 106 features done; built this session: PLT-B-054 (`6aad23c`, gate fixes `bc26451`), settling PLT-B-035; the code gate over `05bf043..6aad23c` also closed PLT-B-053's owed gate. The re-plan found nothing eligible under the default priority.

| | Done: gated | Done: ungated | Open at start | New this session | Open now |
|---|---|---|---|---|---|
| **Batch B3** big tickets | 0 | 0 | 0 | 1 | 1 |
| small tickets | 0 | 0 | 2 | 1 | 3 |
| bugs | 1 | 0 | 3 | 2 | 4 |
| **Phase 1** big tickets | 0 | 0 | 19 | 1 | 20 |
| small tickets | 0 | 0 | 58 | 1 | 58 |
| bugs | 2 | 0 | 34 | 2 | 34 |

Dropped: PLT-B-045 (phase bugs; dropped by the docs gate under D-111). PLT-C-017 was closed by the docs gate, not built, so the phase small row reads 58 open instead of 59. PLT-B-051 was closed by the docs gate (D-124), so the phase bug row reads 34: 34 + 2 new − 2 gated (B-054, B-035) − 1 dropped − 1 closed by the gate = 32 by arithmetic; the status file's own count is authoritative and `ticket-drift` reports no mismatch. Gated: PLT-B-054 and PLT-B-035 (`bc26451`). Ungated: none built.

**Bugs seen**
- PLT-B-066: the `prompt_tokens_estimated` marker is signed into a dropped receipt and not stored on `use_record` (captured).
- PLT-B-067: a job cut short by the host stopping bills an estimated prompt, then becomes `failed(host_restart)` (captured, needs ruling).
- Ideas captured: PLT-F-105, PLT-F-106.
- My own slip, fixed in `ee41663`: a description edit put `: ` into a plain YAML scalar in `api/openapi.yaml` and broke `go generate` for one commit (`55e41cb`); no ticket.

**Verification**
- track-work: worklog rows for PLT-B-054, PLT-B-035, PLT-B-053, the gate, PLT-F-028, PLT-B-051, PLT-C-017 · sync-docs (`closeout-check` exit 0, 6 of 6) · ticket-drift ok (35 warnings, known) · ai-mirror --check not available · naming-check ok (0) · docs-drift exit 0 (39 warnings, known) · budget (counted 3,077,731 of 4,340,874 tokens = 49.6% of the calibrated window; ceiling 70%) · gate (built 1: gated 1, ungated 0; code gate `05bf043..6aad23c` fixed 5, deferred 2; docs gate `638f76b..HEAD` fixed 14, deferred 0; owed: none except the small `ee41663` fix) · docs drift (fixed: size map, F-052 and F-094 rows, 14 gate findings; still open: D-41..D-47 and D-50 stale against depends-on, 31 `schema-table-missing`, 16 `WARN-no-model`, 19 `WARN-source-no-origin`)

**Decisions**
- Needs-ruling rows: none in `implicit-decisions.md`. PLT-B-067 (a bug) awaits your ruling and ties to I-395.
- Added this session (new, `unreviewed`, PLT-B-054):
  - I-402 (new) design: the estimate reuses `gateway.chars_per_token` (default 4), no second setting.
  - I-401 (new) design: the estimate is capped at the model's context size.
  - I-400 (new) design: `estimateTokens` divides first and rounds up, so it cannot overflow.
  - I-399 (new) design: the marker is on the signed receipt only, not in the canonical leaf; `LeafVersion` stays 1.
  - I-398 (new) scope: a call that fails midway keeps prompt 0 and no marker (PLT-F-105).
  - I-397 (new) scope: an empty prompt estimates 0 tokens and is not marked.
  - I-396 (new) design: the embeddings estimate uses the total byte length of all inputs.
  - I-395 (new) scope: the estimate applies to all five aborted sites; a host-stop job awaits PLT-B-067.
- Older `unreviewed` design and scope rows (newest first, 25 shown of 341): I-394 scope, I-392, I-391, I-390, I-389, I-388, I-387, I-357, I-356, I-355, I-354, I-353, I-352, I-351, I-350, I-349, I-348, I-347, I-346, I-345, I-343, I-341, I-340, I-339, I-338; and 316 more in `implicit-decisions.md`.
- 17 older `unreviewed` `copy` rows: see `implicit-decisions.md`.
