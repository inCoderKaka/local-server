# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-159).

## Last session (2026-11-14)

- M0 live run passed on this machine: Ollama 0.31.1, `llama3.2:3b` and `nomic-embed-text`, throwaway data dir created under a pty, `ops/checks/consumer-api-check` 3 of 3 (streamed chat with receipt comment, non-streamed chat with `temperature`, embeddings 768 dims). Host-side `/admin/v1/usage` showed 2 chat and 1 embed request matching the token counts. No prompt text in logs or db. The keychain entry the run made was removed.
- Tracker rows of PLT-B-028, B-031, B-041, B-056, B-084 (fixed) and PLT-C-011 (done) reconciled. PA's requirement file refreshed (not committed, D-145). `go vet` and `go test ./...` pass locally with `CGO_ENABLED=0`.
- Tree: 13 commits ahead of origin, unpushed.

## Waiting on you

- **Push** `main` so CI runs (last GitHub run failed on 2026-11-13, before the fix commits). Push again after each CI fix. M0 is done when CI is green on all three runners with no skipped test (D-158).
- **M1 prerequisites:** standalone Tailscale app on this machine and a second one, MagicDNS and HTTPS certificates on in the tailnet admin console (D-147).

## Next steps

- After a green CI: M1 = PLT-F-038 (D-147, D-148) then PLT-B-008 (D-86 is decided; the bugs row header says so, its body still says needs-decision); gate M0+M1 once at the end of M1 (D-143).
- Backlog dependency cells to reconcile when touched: PLT-F-038 lists PLT-F-036 (M3), PLT-F-032/F-035/F-037 list phase-2 tickets F-029/F-030, PLT-C-004 lists PLT-F-039. Read them against the finish plan, not as blockers.
- Drift flags (parking, no tickets): 8 `stale-decision` warnings D-41..D-47, D-50 and D-141..D-144 (decision date vs later doc edits); `ops/bin/sdlc-skills-install` and `ai-mirror` are absent from this repo.
