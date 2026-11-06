# ops

Repo tooling for the ways of working. Nothing here ships in the local-server binary.

## ops/bin

Four stdlib-only Python 3 scripts (no installs). Each finds the repo root from its own location, so it runs from any directory. All take `--help`, `--track <name> --phase <N>` (default: every `docs/tracks/*/phase_*/` folder), `--strict` (warnings count as findings) and `--quiet` (no summary line).

**Exit codes:** 0 pass, 1 findings, 2 usage or IO error. A finding is one line, `path:line: code: message`, on stdout. A code that starts with `WARN-` is a warning and does not change the exit code unless `--strict`. The summary line goes to stderr. Each run takes well under 2 seconds.

| Script | What it checks | When it runs |
|---|---|---|
| `ticket-drift` | Ticket ids are unique across `backlog.md` and `bugs.md`; every `Depends On` id exists; a done ticket has a worklog row and does not depend on an open one; `implementation-status.md` agrees with the ticket and worklog rows (status word, Model, headline counts, every ticket listed); required fields (Source kind and origin, Model in Sonnet/Opus/Haiku, Pri P0..P3, requirement ref); every open feature and chore has a row in `ticket-sizes.md`; ticket ids in commit subjects exist (`--since <sha or date>`). `--id <ID>` prints what is known about one ticket. | Inside `closeout-check`; before a plan call; whenever a ticket table or the worklog changes. |
| `naming-check` | A word from the glossary's Retired names table used in `docs/**/*.md` or in Go, SQL and YAML sources. Skips the glossary table itself, `docs/execetive_inputs/`, `docs/adr/`, requirement folders and lines that explain a rename. `--since <sha>` limits it to lines added since that commit. | Inside `closeout-check` (over the session's added lines); before a commit that adds docs. |
| `docs-drift` | The mechanical part of the docs drift audit: relative markdown links and `#anchors` resolve; a `planned` marker naming a done ticket, or a `shipped`/`built` claim naming an open one, in `README.md`, `ROADMAP.md`, `docs/user-guide.md` and `docs/developer-guide.md`; ROADMAP batch status against the batch's planned tickets; `decided` decision rows whose depends-on files changed after the row's date (`WARN-stale-decision`); `docs/schema-reference.md` against the migration files under `internal/` (a table missing from the reference is a warning while an open ticket mentions `schema-reference`). | Inside `closeout-check`; by the `docs-drift-auditor` agent. |
| `closeout-check` | The close-out gate. Over `BASE..HEAD` (`--base`, default the end sha of the range in the header of `previous-session-closeout.md`, else `HEAD~1`): every ticket id in a `feat`/`fix` commit subject has a worklog row; `implementation-status.md` was regenerated (committed after, or modified in the tree) since the last `feat`/`fix` commit; the working tree is clean except `implementation-status.md`; and `ticket-drift`, `naming-check` and `docs-drift` pass. Prints one PASS or FAIL line per check and a final summary line. | End of every session, after `/track-work` and `/sync-docs`. `orchestrate-implement` reads its exit code. |

```
ops/bin/ticket-drift                       # every phase
ops/bin/ticket-drift --track platform --phase 1 --since 2b1bb7a
ops/bin/naming-check --since 6e5d49d
ops/bin/docs-drift --strict                # stale-decision warnings fail too
ops/bin/closeout-check                     # base from previous-session-closeout.md
ops/bin/closeout-check --base 6e5d49d
```

`_opslib.py` holds the shared parsing (markdown tables, tickets, worklog, size map) and is not a command.

## Size map

`docs/tracks/<track>/phase_<N>/ticket-sizes.md` maps each feature and chore ticket to `big` or `small` (rule in `docs/tracks/README.md`). Only the orchestrate session edits it. The map's header says which ids it covers; a newer open ticket without a row is a `WARN-unsized`, an older one is a finding.
