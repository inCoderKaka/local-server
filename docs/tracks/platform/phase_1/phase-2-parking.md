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
| 2026-11-14 | The first TLS call to the host's own tailnet address from the host itself hung about 8 minutes after a cold first start, then worked (certificate was ready after 38 s); not reproduced, maybe a macOS self-connect quirk. Watch the first handshake in the second-machine run | `cmd/local-server/serve.go` tailnet listeners | live tailnet self-test |
| 2026-11-14 | Tailnet listeners still send Access-Control-* headers on some answers, against D-148 "No CORS on any listener" | `cmd/local-server/tailnet.go:56-70`, `internal/gateway/errors.go:336`, `internal/api/oas_cfg_gen.go:79` | review-gate M0+M1 |
| 2026-11-14 | A key bound by tailnet name or MagicDNS name stores the status name, not WhoIs `ComputedName` (D-159) | `internal/transport/tailscale/tailscale.go:155-176` | review-gate M0+M1 |
| 2026-11-14 | The tailnet error log lacks the WhoIs error detail | `cmd/local-server/tailnet.go:92-93` | review-gate M0+M1 |
| 2026-11-14 | `doctor` keychain OK says the key opens this host; `MasterKeyVerifier` reports a DB error as "no host row" | `cmd/local-server/doctor.go:315`, `internal/hostkeys/keys.go:203-206` | review-gate M0+M1 |
| 2026-11-14 | D-81 and D-102 still say a keychain timeout fails an existing install; D-114 supersedes it and `docs/modules/host.md:14` quotes the old rule | `req_001.decisions.md` | review-gate M0+M1 |
| 2026-11-14 | `UpdateAccessKey` looks up the node before the revoked and expired checks; setting copy says unnamed nodes are "logged as unknown" | `internal/admin/consumers/keys.go:333-337`, `cmd/local-server/startup.go:104` | review-gate M0+M1 |
| 2026-11-14 | `Unload` comment claims synchronous eviction, but a nil return with the model still loaded can overcommit the memory budget; its poll interval is hard-coded as RequestTimeout/300 (D-38); no unit test for the 0.31.1 overflow markers | `internal/engine/engine.go:30`, `internal/engine/ollama/ollama.go:271-292` | review-gate M0+M1 |
| 2026-11-14 | The scheduler holds `dispatchMu` across `Engine.Unload` polling, so admissions stall up to `engine.ollama_request_timeout` while a model is still in use; mitigation: lower that setting. Settle with it whether an unconfirmed eviction is a failure and is logged | `internal/scheduler/scheduler.go:503,534`, `lifecycle.go:43,98,125,151` | review-gate M0+M1 |
| 2026-11-14 | Module docs miss the Unload wait, OpenReadOnlyImmutable and the D-112 rule; backup and readonly comments are wrong when only -wal exists (-shm is created) | `docs/modules/engine.md`, `storage.md`, `limits.md`, `cmd/local-server/backup.go:404-406`, `internal/storage/readonly.go:73-75` | review-gate M0+M1 |
| 2026-11-14 | `tailscale.com` is marked indirect in go.mod though imported directly | `go.mod:167` | review-gate M0+M1 |
| 2026-11-14 | `consumer-api-check` sends the key over http to any non-.ts.net host | `ops/checks/consumer-api-check:58-60` | review-gate M0+M1 |
| 2026-11-14 | PLT-C-011: only Ollama 0.31.1 ran; the 0.12.6 minimum is unexercised | `internal/engine/ollama/config.go:13` | review-gate M0+M1 |
| 2026-11-15 | `go ./...` does not skip `web/**/node_modules`, so a future npm package shipping a `.go` file would break `go build ./...` | `Makefile`, CI | build PLT-F-032 |
| 2026-11-15 | vitest 5 and jsdom 31 do not list Node 25 in their engines (npm only warns) | `web/package.json` | build PLT-F-032 |
| 2026-11-15 | `lld.md` module map does not list `webui`, which `tools/archcheck/config.go` mirrors | `docs/design_docs/lld.md` | build PLT-F-032 |
| 2026-11-15 | `npm run dev` proxy to a running admin listener left out | `web/admin/vite.config.ts` | build PLT-F-032 |
| 2026-11-15 | Consumers list has no row-click suspend; the `warnings[]` field on create, issue and update answers is never shown | `web/admin/src/consumers/`, `invites/` | build PLT-F-033 |
| 2026-11-15 | No API maps a `grant_id` to its consumer (Rules page shows a raw grant id); `GET grant` 404 does not tell "no consumer" from "no grant yet"; a stored grant expiry with seconds is cut to minutes on re-save | `web/admin/src/policy/RulesPanel.tsx`, `internal/admin/grants/errors.go`, `grants/GrantForm.tsx` | build PLT-F-034 |
