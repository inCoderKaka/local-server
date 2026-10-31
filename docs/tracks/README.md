# Tracks: per-track work tracking

Adapted on 2026-10-30 from the personal-assistant repo's tracks convention (v1), trimmed to what local-server uses. A **track** is a product area with its own folder of tracking files. local-server has one track today, `platform`.

## Track registry

| Prefix | Track | Folder | Status |
|---|---|---|---|
| `PLT` | Platform: the whole local-server app (gateway, control plane, scheduler, ledger, notifier, host-metrics, adapters, UIs, installers, CI, docs) | `platform/` (**active phase: `phase_1/`**, opened 2026-10-30 for req_001) | **active, current focus** |

Adding a track means adding a row here and creating its folder from the file set below. Keep the `` `<folder>/` (**active phase: `phase_N/`**) `` shape in the Folder cell, because tools find each track's active phase with it. Unclear ownership goes to `PLT`.

## Ticket IDs

`<TRACK>-<TYPE>-<NNN>`, e.g. `PLT-F-001`, `PLT-C-001`, `PLT-B-001`.

- **Types:** `F` feature, `B` bug, `C` chore.
- `NNN` is sequential **per (prefix, type)**. It is assigned once, immutable and never reused. The next id is the maximum across every phase of every track whose registry row names that prefix, plus one.
- The owning track of a ticket is the one whose file holds its row, never its prefix.

## Phases

A track splits its files into **phase folders** (`<track>/phase_N/`).

- **Active phase:** exactly one per track, named in the registry row. Skills and runners write to the active phase.
- **Frozen phases:** read-only history. Rows never move between phases. Ticket ids continue across phases.
- **Only the user opens a phase.** No skill, agent or orchestrator creates one.
- **Requirements** live in `phase_N/requirements/` as `req_<NNN>.md`, with `req_<NNN>.decisions.md` (and optionally `req_<NNN>.formal.md`) beside them. A phase holds several requirements, and numbering restarts per phase, so a requirement is named by track + phase + number.

## Per-phase file set

Each phase folder contains these files:

- **`design.md`:** the design summary per requirement. It points to the decisions log and ADRs and never copies them. ADRs are global, in `docs/adr/`.
- **`design-status.md`:** where each requirement stands in the design stages. Its only writer is the `orchestrate-design` agent.
- **`backlog.md`:** the feature registry, in three sections in order:
  - `## 1. Pending`, split into four quadrants by Impact × Effort: Q1 High/Medium + Quick win, Q2 High/Medium + Rich, Q3 Low + Quick win, Q4 Low + Rich.
  - `## 2. Done but not shipped`.
  - `## 3. Shipped`.

  Pending columns: `ID | Feature | Artifact | Pri | Impact | Effort | Depends On | Sub-tasks | Ref | Code Rev. | Sec. Rev. | Model | Notes | Source`. Done and Shipped rows drop Pri/Impact/Effort.

  A flat **`## Chores`** table at the end (`ID | Type | Title | Pri | Status | Depends on | Notes | Source`) holds `C` tickets.

  Statuses: `idea → designed → building → done → shipped`, or `dropped` (with a reason, the text struck through, never deleted).

  **Model is load-bearing:** the ticket is built on the model it names (Haiku/Sonnet/Opus/Fable). UI-heavy tickets build on Opus.
- **`bugs.md`:** the bug registry (`ID | Pri | Status | Description / Ref | Source`). Statuses: `reported → confirmed → fixed → shipped`, or `dropped`. Default P2.
- **`worklog.md`:** the work ledger: what happened, when, root causes and verification. A ticket id may appear on several rows, one per session.
- **`next-steps.md`:** the ordered queue for the next session, each item pointing at tickets.
- **`implementation-status.md`:** built by the `implementation-status-reporter` agent only.

## Decisions log format

- **Status words:** exactly `open`, `decided` or `deferred`. A chat resumes at the first `open` item.
- **Row types:** `D-NN` decisions (ID, Date, Decision, depends-on, Status), `F-NN` facts and `Q-NN` open questions. IDs are sequential and never reused.
- **Deferred rows:** a `deferred` row names the ticket or slice it blocks.
- **Hard gate:** while any `open` row exists, nothing in that requirement is built. The gate is per requirement.
- **Supersede protocol:** never reword a relied-on decision. Strike it through, set it to `decided, superseded by D-MM`, add the new row, and list the affected tickets, ADRs, docs and `@planned` blocks.
- **Stage prefixes:** `HLD:`, `LLD:` and `AI:` mark rows from those design stages. `design-close: … docs synced` rows record doc syncs.
- **Coverage matrix:** maps every requirement line to decisions and tickets.

## Ticket fields

Requirement-driven tickets carry these fields inside existing cells. Every row also has a **Source** cell, always the last column.

| Field | Where | Format |
|---|---|---|
| Requirement ref | `backlog.md` Ref cell · Chores Notes · `bugs.md` Description | `req_N D-NN[, D-NN]` |
| Plan position | same cell as the requirement ref | `P<phase>/B<batch>/S<slice>`, e.g. `P1/B1/S3` |
| Model | the **Model** column · `Model: <X>` in Chores Notes and bug Descriptions | Haiku / Sonnet / Opus / Fable |
| How to verify | Notes (Chores: Notes; bugs: Description) | `verify: <one line>`. It uses curl, CLI, tests or the API, **never a browser tool** |
| Source | last column | `<kind> <origin>`. The kind comes first: `req_N` · `design` · `review-gate` · `build` · `user` · `drift-audit` · `legacy`. The origin names exactly where the ticket came from: the decision rows (`req_001 D-38, D-54`), the design section (`design HLD §10`), the gate run and finding (`review-gate set B 9853112..ef86df3 A#5`), the ticket whose build surfaced it (`build PLT-F-001`), or the date of the user's ask (`user 2026-10-31`) |

- **Plan hierarchy:** phase > batch > slice > feature > subfeature. "Wave" is never a plan level. It means OpenCode handoff files only.
- **Requirement-driven rows:** a row is requirement-driven if a cell contains a `req_\d+` token.
- **Work order:** planned tickets (Source `req_N` or `design`) come first, by Pri.
- **Public docs:** `User-visible: yes` in Notes marks a ticket that needs a public-docs `@planned` block. The public docs (D-63) currently mark planned features with a page-level status banner; no `@planned` blocks are authored yet.

## What stays global

`docs/design_docs/` (HLD, LLD), `docs/architecture.md` (pointer), `docs/adr/`, `docs/glossary.md`, `docs/legal/`, `docs/execetive_inputs/`, `README.md`, `ROADMAP.md`, `docs/user-guide.md`, `docs/developer-guide.md`.

## Skills / workflow mapping

- `/capture-feature` appends to the active phase's `backlog.md` (default P3). `/capture-bug` appends to `bugs.md` (default P2).
- `/track-work` writes `worklog.md`.
- `/build-feature` builds one ticket at its Model, then runs the review gate.
- `/sync-docs` reconciles the global docs and the active phase's files with the code.
- `orchestrate-design` writes `design-status.md`. `orchestrate-implement` writes `implicit-decisions.md`, `implementation-handoff.md` and `previous-session-closeout.md`.
