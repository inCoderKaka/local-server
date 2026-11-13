# platform — phase 2 parking

One line per item found during phase 1 that is outside the finish line (`finish-plan.md`, req_001 D-142). No ids, no sizing, no decisions. Phase 2 design reads this file and the phase 1 backlog and bug rows marked out of scope.

| Date | What | Where | Found by |
|---|---|---|---|
| 2026-11-13 | A reset link the host issues for a consumer who lost a password or passkey; today the host deletes and re-invites | admin UI, `internal/consumerauth` | req_001 D-154 |
| 2026-11-13 | Lawyer-reviewed v2 of the Terms and Privacy texts: operator legal name, governing law, minimum age, liability, re-consent on a new version, key-only consumers | `docs/legal/` | req_001 D-153 |
| 2026-11-13 | Consumers need no Tailscale and no account: public mode over Tailscale Funnel, where the host keeps an account and a consumer needs only the URL and a key (PLT-F-039, D-24) | `internal/transport` | user ask 2026-11-13 |
| 2026-11-13 | No one needs a Tailscale account: an own coordination server (Headscale, or the own-relay seam of D-02) that local-server joins automatically; a central service to operate, likely with embedded Tailscale (PLT-F-040) | `internal/transport`, `internal/directory` | user ask 2026-11-13 |
