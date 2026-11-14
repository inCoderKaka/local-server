# platform — phase 2 parking

One line per item found during phase 1 that is outside the finish line (`finish-plan.md`, req_001 D-142). No ids, no sizing, no decisions. Phase 2 design reads this file and the phase 1 backlog and bug rows marked out of scope.

| Date | What | Where | Found by |
|---|---|---|---|
| 2026-11-13 | A reset link the host issues for a consumer who lost a password or passkey; today the host deletes and re-invites | admin UI, `internal/consumerauth` | req_001 D-154 |
| 2026-11-13 | Lawyer-reviewed v2 of the Terms and Privacy texts: operator legal name, governing law, minimum age, liability, re-consent on a new version, key-only consumers | `docs/legal/` | req_001 D-153 |
| 2026-11-13 | Consumers need no Tailscale and no account: public mode over Tailscale Funnel, where the host keeps an account and a consumer needs only the URL and a key (PLT-F-039, D-24) | `internal/transport` | user ask 2026-11-13 |
| 2026-11-13 | No one needs a Tailscale account: an own coordination server (Headscale, or the own-relay seam of D-02) that local-server joins automatically; a central service to operate, likely with embedded Tailscale (PLT-F-040) | `internal/transport`, `internal/directory` | user ask 2026-11-13 |
| 2026-11-14 | The tailnet state is read once at start: a Tailscale that comes up or logs in after local-server started keeps the tailnet listeners off until a restart (a service that starts before Tailscale connects hits this); a watcher that retries and binds later is not built | `cmd/local-server/serve.go`, `cmd/local-server/tailnet.go` | PLT-F-038 build |
| 2026-11-14 | The calling tailnet node is in the log only (`tailnet request`), not on `use_record` or the audit trail: a use row is immutable and in the ledger leaf, so a column there changes receipts | `internal/usage`, `internal/gateway` | PLT-F-038 build |
| 2026-11-14 | `doctor` shows Tailscale's version string but does not name the macOS variant (standalone, App Store, Homebrew) that D-147 says other variants are reported as | `cmd/local-server/doctor.go`, `internal/transport/tailscale` | PLT-F-038 build |
