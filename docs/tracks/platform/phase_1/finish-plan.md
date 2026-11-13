# platform / phase_1 — finish plan

Written 2026-11-12, updated 2026-11-13. Decisions: req_001 D-141..D-158. This file overrides `next-steps.md` for work order and overrides any skill that says "capture everything". Every chat on this repo reads this file first and works on the first milestone that is not done.

## Finish line (D-141)

Phase 1 is done when all six statements are true on real machines, not only in tests:

1. A host installs the binary on macOS with one command and it runs as a background service against a real Ollama.
2. The host opens the admin UI, sees host total, share budget and other apps' use on every page, sets the share, creates a consumer, a grant with model and context limits, and an access key.
3. A consumer on another machine reaches the host over Tailscale and gets chat (streamed and not) and embeddings with that key. personal-assistant is not a blocker: its requirement file is kept current instead (D-145).
4. The consumer signs up from an invite, logs in to the consumer page, sees limits, usage and keys, and files a resource request with comments. The host approves or rejects it in the admin UI.
5. The host sees current usage and usage over time per consumer.
6. A tagged release exists with checksums, and `README.md`, the user guide, the developer guide, the HLD and the LLD describe what is built.

Nothing else is phase 1.

## Scope

### In (the whole remaining build list)

| Milestone | Tickets |
|---|---|
| M0 Prove the core | PLT-C-011 (real Ollama run), PLT-B-031, PLT-B-028, PLT-B-041, PLT-B-084, PLT-B-056, first push and first CI run |
| M1 Reachable | PLT-F-038 (Tailscale transport, D-147, D-148), PLT-B-008 (remote admin login, D-86), the three calls from a second machine |
| M2 Host UI | PLT-F-032, PLT-F-033, PLT-F-034, PLT-F-035 |
| M3 Consumers | PLT-F-027, PLT-F-028, PLT-F-036, PLT-F-037 |
| M4 Ship | PLT-B-001 (pin CI actions, D-88), PLT-C-003, PLT-F-043 |
| M5 Docs and close | PLT-C-004, HLD and LLD reconciled with the code, user guide, developer guide, `/phase-retro` |

Narrowed tickets: PLT-F-032, PLT-F-035 and PLT-F-037 are built without the notification, webhook and data-sharing pages, because those backends are out. The admin UI shows a count of pending resource requests instead of live notifications (polled, D-157).

Kept inside those tickets (D-154): keys bound to a tailnet node, consumer change-password, add-passkey and settings page, `install.ps1`. Parked: a host-issued login reset link.

### Out (moved to phase 2, not built, not discussed)

- **Planned tickets:** PLT-F-029, F-030, F-031 (notifications, webhooks, desktop), PLT-F-039 (public Funnel mode), PLT-F-040 (tsnet), PLT-F-045 (bundles), PLT-F-046, F-047, F-048 (platform sync and sharing grants).
- **Every spin-off feature** PLT-F-049..PLT-F-113 that is not built today, and chores PLT-C-006, C-008, C-016.
- **Every open bug not named under "In".** This includes the seven set A gate decisions (PLT-B-077..083), the aborted-call billing set (PLT-B-066, B-067, PLT-F-105, F-106), the Windows service bugs (PLT-B-068..071) and PLT-B-034 (public mode only).
- Built features that are out of the finish line (backups, service, sleep lock, jobs) stay in the binary as they are. They get no more work in phase 1.

## Scope freeze (D-142)

- A new item enters phase 1 only if it **breaks one of the six finish-line statements** or is **a security hole a consumer or a tailnet peer can reach**. It is then fixed in the same milestone, with one bug row.
- Anything else found while building or gating is **one line** in `phase-2-parking.md` (what, where, found by). No ticket id, no sizing, no backlog row, no decision question, no D-row, no I-row.
- `capture-bug`, `capture-feature`, the builder agents and the review gate all follow this rule here.
- The user is asked a question only when a finish-line ticket cannot be built without the answer.
- The in-scope list above can shrink. It grows only by the first bullet.

## Review gate (D-143)

- No per-ticket gate. **One gate at the end of each milestone** over that milestone's whole commit range, M1 to M4. M0 is gated with M1. M5 is docs and gets no code gate.
- The gate's fixer implements only findings that meet the freeze rule. Every other approved finding is a parking line.
- The ungated commits `d6f7d6c` and `8fea837` and the owed docs gate over `7378f87..HEAD` are released from the gate: their features are outside the finish line.

## Budget and timeline (D-144)

Measured: 24 past sessions cost about $514 at list prices, which is 17.7 five-hour windows (one window = about $29). That built 26 planned features with every ticket gated. The user reports a weekly cap of about 8 to 10 full windows; the plan uses 8.

| Milestone | Build | Gate | Windows | Target end |
|---|---|---|---|---|
| M0 Prove the core | 1 | with M1 | 1 | 13 Nov |
| M1 Reachable | 1.5 | 0.5 | 2 | 16 Nov |
| M2 Host UI | 3 | 0.5 | 3.5 | 21 Nov |
| M3 Consumers | 3.5 | 0.5 | 4 | 27 Nov |
| M4 Ship | 1.5 | 0.5 | 2 | 30 Nov |
| M5 Docs and close | 1 | — | 1 | 2 Dec |
| Contingency (about 20%) | | | 2.5 | |
| **Total** | | | **16** | **2 Dec 2026** |

- **Hard stop: 20 windows.** If the count reaches 20 before M5 ends, stop building and ask the user which cut to take, in this order: (1) passkey login, password only stays; (2) PLT-F-037 reduced to limits, usage and requests; (3) Linux and Windows release artefacts marked untested.
- The windows used so far are recorded in the table at the bottom, one row per chat, measured with `session_cost.py`.
- The calibration covers Sonnet, Opus and Haiku only. Run build chats with the main chat on Sonnet and builders on each ticket's Model; a main chat on a larger model costs more per window than this table assumes.

## Rules for each chat

1. Read this file and `implementation-handoff.md`. Read nothing else until a ticket needs it. Do not read `implementation-status.md`, `implicit-decisions.md` or `backlog.md` whole.
2. Take the next unbuilt ticket of the current milestone. One milestone slice per chat; do not start a second milestone in the same chat.
3. Stop starting work at 80% of the window. Then: `/track-work`, `/sync-docs`, rewrite the handoff, add the chat's row to the table below, commit.
4. `implementation-status.md` is rebuilt only at a milestone end, not per chat.
5. A milestone is done when its "done when" check below passes and its gate has run.
6. Implicit decisions are logged only when they change user-visible behaviour, an API or stored data.

## Milestone checks

- **M0 done when:** `local-server serve` against the Ollama on this machine answers a streamed chat, a non-streamed chat with `temperature`, and an embeddings call with a key made through the admin API; usage shows the three calls; it runs on a throwaway data directory with `llama3.2:3b` and `nomic-embed-text` (D-149); PA's requirement file is rewritten (D-145); CI is green on GitHub on all three runners with no skipped test (D-158).
- **M1 done when:** the same three calls succeed from a second machine over the tailnet and the admin logs in remotely. The user runs the session's check script on the second machine; the pass is read from host-side records (D-146).
- **M2 done when:** statement 2 and statement 5 pass in a browser with no curl, proven by a Playwright test (D-152).
- **M3 done when:** statement 4 passes in a Playwright test, and once by the user in a browser on a second machine, read from host-side records (D-146).
- **M4 done when:** `v0.1.0-rc.1` builds all six artefacts with checksums and the install script sets up a working service on a clean macOS user account; then `v0.1.0` (D-156).
- **M5 done when:** statement 6 passes and `/phase-retro platform 1` is written.

## Needed from the user

| Before | What |
|---|---|
| M0 | Make the repo public, then push so CI runs (D-156). Push again after each CI fix commit. |
| M1 | Install the standalone Tailscale app (D-147) on this machine and on a second machine in the same tailnet; turn on MagicDNS and HTTPS certificates in the tailnet's admin console. Run the M1 check script on the second machine. |
| M3 | Click through the M3 checklist in a browser on the second machine. |
| M4 | Push the `v0.1.0-rc.1` tag, run the install command on a clean macOS user account, then push `v0.1.0`. |

Nothing else waits on the user: the legal drafts are approved (D-153), the artefact question is answered (D-156) and no migration go is needed (D-149).

## Progress

| Date | Chat | Milestone | Windows used | Running total | Result |
|---|---|---|---|---|---|
| 2026-11-12 | plan | — | 0.1 | 0.1 | this plan, D-141..D-144 |
