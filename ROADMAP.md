# Roadmap

Phase 1 is fully designed ([HLD](docs/design_docs/hld.md), [LLD](docs/design_docs/lld.md)) and is being built in the order below. Each batch builds on the ones before it. The detailed tickets live in [`docs/tracks/platform/phase_1/`](docs/tracks/platform/phase_1/next-steps.md).

## Phase 1 — share a host's AI with invited consumers

| Batch | What it delivers | Status |
|---|---|---|
| B1 Foundations | Go module and a 3-OS CI definition with boundary, race, fuzz and vulnerability checks; the OpenAPI 3.1 contract; the storage foundation (single writer, migrations, data catalog); telemetry; runtime settings; host identity and sealed secrets | built |
| B2 Core domain | Engine port with the Ollama adapter; identity and grants; audit log; domain events; access keys; policy rules; host metrics; pause sharing and quiet hours | built |
| B3 Inference path | Scheduler with memory admission; usage records, Merkle ledger and signed receipts; checkpoints and proofs; quotas and rate limits; the OpenAI-compatible gateway with streaming; jobs API; usage rollups; host status, capabilities and models endpoints; startup and the `doctor` command | built |
| B4 Control plane and accounts | Admin API; consumers, invites and keys; grants, models and policy; consumer accounts with password and passkey login; resource requests; terms and privacy drafts | built |
| B5 Notifications | In-app notifications, Standard Webhooks via an outbox, desktop notifications | planned |
| B6 Web UIs | Admin UI (shell, dashboard, consumers, invites, keys, grants, models, policy rules, usage over time, audit log, settings and resource requests built; notification and webhook pages planned) and consumer web page (sign-up from an invite with consent, password and passkey login, limits, usage, keys, resource requests and settings built; notification and webhook pages planned) | built, except the planned notification and webhook pages |
| B7 Transport | System Tailscale, opt-in public mode (Funnel) with mandatory protections, optional embedded tsnet | planned: the system Tailscale part is built and tested on one machine, not yet on a second machine; Funnel and tsnet are not built |
| B8 Packaging | Background service, sleep lock, release pipeline with SLSA provenance, install and upgrade scripts | in progress: the sleep lock and the background service built (held while sharing; per-user by default, system mode optional; the Windows adapters are not yet run on Windows); the release pipeline and the install scripts planned |
| B9 Off-host data | Encrypted backups to a folder, R2 or Google Cloud Storage; signed export and import; opt-in platform sync (Postgres); sharing grants | in progress: encrypted backups to a folder or a bucket and offline restore built (the real-bucket test has not run); signed export and import, platform sync and sharing grants planned |
| B10 Docs | Host setup in the user guide; README install steps | planned |

## Later phases (designed as seams, not yet scheduled)

- **Discovery and marketplace:** a central directory of hosts with benchmarks and smart host selection. Proposed on Google Cloud ([HLD §9](docs/design_docs/hld.md#9-future-scale-out-and-the-central-service-proposal-not-decided)).
- **Billing:** plans and credits, built on the usage ledger and receipts that Phase 1 already produces. A commercial-ready transport (Cloudflare Tunnel with our own end-to-end encryption, or our own relay) comes first, since Tailscale's free plan is non-commercial.
- **Failover:** client-side failover across several hosts.
- **More engines:** llama.cpp and MLX adapters.
- **More channels:** email and Telegram notifications.
- **Packaging:** brew and winget packages, auto-update.
- **Telemetry:** an OpenTelemetry adapter.
- **More resource kinds:** non-inference resources.
