# Context — last updated 2026-10-30
Truth lives in: `docs/tracks/platform/phase_1/design-status.md` (stage + resume notes), `.../requirements/req_001.decisions.md` (all decisions D-01..D-45), `.../requirements/req_001.md` (reqs), `LICENSE` (Apache-2.0, D-18).

## Immediate goal
Finish platform phase_1 req_001 design (stages 0-3 done, 4 skipped, 5 design-close not started), then ticketize. No code exists yet.

## Next steps (ordered)
1. Next chat: `/orchestrate-design platform 1 1` -> `design-close ... discussion`, then `design-close ... design-review` (glossary, legal dir, decision traces: see design-status.md#Position + next action).
2. Write PA's requirement file at `~/files/Projects/personal-assistant/docs/tracks/platform/phase_2/requirements/req_001.md` (D-17). Create the phase_2 folder (user allowed it). Do NOT commit it. Write from local-server's perspective; cite API contract D-32/D-33 and PA's LLMProvider port (F-04, D-03).
3. Verify unsourced items before ticketize (list in design-status.md resume notes).
4. `ticketize`, then `/orchestrate-implement platform 1`.

## Open threads & gotchas
- Repo has no CLAUDE.md / AGENTS.md / docs/tracks/README.md; orchestrator falls back to its own rules. Consider creating them in design-close.
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
