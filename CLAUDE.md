# local-server

local-server lets a host share their machine's AI inference (Ollama) with invited consumers and client apps over an encrypted transport (Tailscale), with keys, grants, metering, signed usage receipts and resource requests. It is a single Go binary per OS with SQLite and built-in web UIs. Licence: Apache-2.0.

## Where things are

- **Tracks convention:** `docs/tracks/README.md`. **Current focus:** the `platform` track, active phase `docs/tracks/platform/phase_1/`.
- **Requirements and decisions:** `docs/tracks/platform/phase_1/requirements/req_001.md` and `req_001.decisions.md`. The decisions log is the source of truth.
- **Design:** `docs/design_docs/hld.md` and `docs/design_docs/lld.md` (planned until built; `docs/architecture.md` only points to them), `docs/adr/`, `docs/glossary.md` (search here before naming anything), `docs/legal/`, `docs/execetive_inputs/`.
- **Public docs:** `README.md`, `ROADMAP.md`, `docs/user-guide.md` (hosts and consumers, plain language), `docs/developer-guide.md` (client-app developers). They describe only software, never ways of working, and mark planned features as planned (D-63).
- **Work queue:** the active phase's `next-steps.md` and `backlog.md`.

## Rules

- **Commits:** standing authorization to `git commit` without asking. Stage exact paths only. Never `git add -A`, `git add .`, `git commit -a`, `--amend` or `--no-verify`. **Never push.** The user pushes.
- **PA:** never modify the personal-assistant repo's code from here (req_001 D-04). Only its requirement file is written from here, and it is not committed.
- **Decisions:** decide nothing the log leaves open. Ask the user, and record the answer as a `D-NN` row.
- **Names:** one word, one meaning. Follow `docs/glossary.md`.
- **Configuration:** every time, retention, TTL, retry and threshold value is a runtime setting owned by one party, never hard-coded (D-38).
- **Content privacy:** prompts and responses are never logged or stored by default (D-31, D-40).
- **Builds:** `CGO_ENABLED=0`. The only exception is the CI race-detector test job (`CGO_ENABLED=1 go test -race`, D-61). Never strip or alter a built darwin binary (F-07).

## Close-out (after building or ticketing)

1. `/track-work` logs the session in the active phase's `worklog.md`.
2. `/sync-docs` reconciles the docs with the code.
3. The `review-gate` agent runs over the change. It is the only sanctioned review. Commit what it leaves pending.
