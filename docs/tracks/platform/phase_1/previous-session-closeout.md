# platform / phase_1 — previous session closeout

Session: 2026-11-15 · passive · 2002da6..4569d61 · tree clean · 72 unpushed commits · migrations: not available

**Open items**: M2+M3 gate final report never arrived (fixes committed, "gated" inferred); CI, Pester, `release.yml` and the Windows script never run; PLT-F-038 waits on the second-machine check; ROADMAP B8 flagged by docs-drift (true until the rc tag).

**Next steps**: Waiting on you: push `main`; M1 and M3 second-machine checks; after green CI the `v0.1.0-rc.1` tag, the macOS 15 and 26 checklist, the clean-account install, then `v0.1.0`; rulings on system-mode root and service-before-first-run; I-455. Then M5 (PLT-C-004, guides, HLD and LLD, `/phase-retro`).

**Where we stand**: phase_1 · batch B8 · slice S46: built this session: PLT-B-001, PLT-C-003, PLT-F-043.

| | Done: gated | Done: ungated | Open at start | New this session | Open now |
|---|---|---|---|---|---|
| **Batch B8** big tickets | 2 | 0 | 2 | 0 | 0 |
| small tickets | 0 | 0 | 1 | 0 | 1 |
| bugs | 0 | 0 | 0 | 0 | 0 |
| **Phase 1** big tickets | 2 | 0 | 15 | 0 | 13 |
| small tickets | 0 | 0 | 57 | 0 | 57 |
| bugs | 1 | 0 | 42 | 0 | 41 |

Dropped: none. Ungated: none.

**Bugs seen**: none ticketed (D-142); 14 parking lines in `phase-2-parking.md`, including the misleading `service install` hint (`cmd/local-server/service.go:271`).

**Verification**: track-work (rows PLT-B-001, C-003, F-043) · sync-docs (done per ticket) · ticket-drift exit 0 · ai-mirror --check not available · naming-check exit 0 · drift audit: docs-drift exit 1 (ROADMAP B8, 12 stale-decision warnings), fixable items fixed · budget (counted 3.9M of 4.65M tokens, about 63% of the window) · gate (built 3: gated 3, ungated 0, owed: none; M2+M3 gate report outstanding) · docs drift (fixed: backlog, bugs, worklog, finish-plan, README, glossary rows | still open: above)

**Decisions**
- Needs-ruling: I-455 (design, PLT-F-035) e2e in-process harness; recommended yes.
- Added this session (all unreviewed): I-467 design passkey per-address ceremony limit; I-468 design CI pins; I-469 design Dependabot weekly; I-471 design release workflow two jobs; I-472 design `-trimpath` and tag as version; I-473 design release asset names and pre-release rule; I-474 copy release notes; I-475 design Ollama and Tailscale offered after yes; I-476 design install settings; I-477 design first run judged from doctor host line; I-478 design uninstall; I-479 design install as upgrade; I-480 design prompts default no, no `--yes`.
- Older unreviewed design and scope rows: see `implicit-decisions.md`.
