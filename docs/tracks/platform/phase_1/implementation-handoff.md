# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-159).

## Last session (2026-11-14, M1 build)

- PLT-F-038 built (Tailscale adapter over `tailscale.com/client/local`, tailnet HTTPS listeners 8484/8485, optional remote admin 8486, keys bound to a node, `doctor` check, degrade to localhost) and PLT-B-008 fixed (HTTPS admin login, Secure cookie). Binary +2.4 MB (31.3 to 33.6 MB), within D-147.
- Live self-test on this machine over the real tailnet (`kakas-macbook-air.tailcfd67b.ts.net`): `ops/checks/consumer-api-check` 3 of 3 over https, `tailnet request` log lines carry node ID and name, usage matches, bound key works from the node and a bogus node is refused, remote admin login over https sets a `Secure` cookie, no prompt text in log or db. The run's data dir and keychain entry were removed.
- It found PLT-B-085 (`/metrics` open to tailnet peers), fixed in `a2144f3`. The M0+M1 gate (`8de56f9..HEAD`) ran and fixed two things (`dd607b2`); its 12 other findings are parking lines.
- Tree: unpushed commits ahead of origin (`git status -sb`).

## Waiting on you

- **Push** `main` so CI runs. M0 is done when CI is green on all three runners with no skipped test (D-158).
- **M1 second-machine check (deferred by you):** standalone Tailscale on a second machine in the tailnet, then `ops/checks/m1-checklist.md`. PLT-F-038 stays in-progress and M1 not done until it passes, read from host-side records (D-146). MagicDNS and HTTPS certificates are already on (doctor reports them).

## Next steps

- M2 Host UI (PLT-F-032..F-035) is next once you accept M1 as built; one milestone per chat (finish plan rule 2).
- Backlog dependency cells to read against the finish plan, not as blockers: PLT-F-038 lists PLT-F-036 (M3); PLT-F-032/F-035/F-037 list phase-2 tickets F-029/F-030; PLT-C-004 lists PLT-F-039.
- Drift: 8 `stale-decision` warnings D-41..D-47, D-50 (docs-drift); `ops/bin/ai-mirror` and `sdlc-skills-install` are absent from this repo.
