# Context — last updated 2026-10-30
Truth lives in: `docs/tracks/platform/phase_1/design-status.md` (stage + resume notes), `.../requirements/req_001.decisions.md` (all decisions D-01..D-50), `.../requirements/req_001.md` (reqs), `LICENSE` (Apache-2.0, D-18).

## Immediate goal
req_001 design done (stages 0–3, 5, 6 done; 4 skipped) and ticketized: 52 tickets PLT-F-001..048, PLT-C-001..004; PLT-C-005 (close-out) done. No code exists yet.

## Next steps (ordered)
1. `/orchestrate-implement platform 1` from next-steps.md B1 (PLT-C-001).
2. The user approves the legal drafts (PLT-C-002) before PLT-F-027 builds.
3. Decisions owed before PLT-F-001/F-015/F-016/F-018 build: the receipt on the metering-failure path, which checkpoint a receipt cites, which key signs checkpoints, and SLO targets.

## Open threads & gotchas
- CLAUDE.md and docs/tracks/README.md exist (6e8f4eb); PA requirement file written, uncommitted (D-50).
- Harness env may say "not a git repository"; it is one.
- Decisions log is script-edited: keep each table row on its own line (an earlier bug joined two rows).
- Never modify PA code from this repo (D-04).
- Remote: private GitHub inCoderKaka/local-server (main). `.claude/context/` is committed by user choice. Commit exact paths only, never `git add -A`; leave pushing to the user.

## User preferences
- Everything behind adapters and switchable; everything dynamically configurable by its owning party (D-38).
- Decentralized data by default; centralized only by user choice (D-41).
- Human-readable docs, short table prefixes (D-44); legal discussed for current phase only.
- Big decisions: pros/cons with sources; recommendations are fine to take.

## Eventual goals
Future direction = seams only (D-45): see decisions log. Deferred scope trims (email/Telegram notifications, brew/winget, auto-update) are recorded in the log.
